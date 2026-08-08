package ipc

import (
	"encoding/gob"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lestex/torrnado/internal/engine"
)

// Client is a connection to a running daemon. It is safe for concurrent
// use: several goroutines may make calls while another reads events.
type Client struct {
	conn net.Conn
	enc  *gob.Encoder

	// writeMu serializes calls, for the same reason the server needs one:
	// a gob stream cannot be written by two goroutines at once.
	writeMu sync.Mutex

	// mu guards seq and pending. pending maps a call's sequence number to
	// the channel its caller is blocked on, which is how a reply arriving
	// on a shared connection finds the one goroutine waiting for it.
	mu      sync.Mutex
	seq     uint64
	pending map[uint64]chan *Response

	events    chan engine.Event
	closed    chan struct{}
	closeOnce sync.Once
}

// Dial connects to the daemon listening on socketPath.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		enc:     gob.NewEncoder(conn),
		pending: map[uint64]chan *Response{},
		events:  make(chan engine.Event, 1),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Events returns the stream of state pushed by the daemon. The channel is
// closed when the connection ends.
func (c *Client) Events() <-chan engine.Event { return c.events }

// Close disconnects from the daemon.
func (c *Client) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.conn.Close()
}

// readLoop is the only reader of the connection. It sorts what arrives:
// replies go to whichever caller is waiting on that sequence number,
// events go to the event channel.
func (c *Client) readLoop() {
	defer close(c.events)

	dec := gob.NewDecoder(c.conn)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			// The connection is gone. Anyone waiting for a reply will
			// wait forever unless they are told, so tell them.
			c.failAllPending(fmt.Errorf("daemon connection closed: %w", err))
			return
		}

		switch msg.Kind {
		case kindReply:
			if msg.Resp == nil {
				continue
			}
			c.mu.Lock()
			ch, ok := c.pending[msg.Resp.Seq]
			delete(c.pending, msg.Resp.Seq)
			c.mu.Unlock()
			if ok {
				ch <- msg.Resp
			}
		case kindEvent:
			if msg.Event != nil {
				c.pushEvent(*msg.Event)
			}
		}
	}
}

// pushEvent delivers an event without blocking, dropping the previous one
// if the consumer has not kept up - the same rule the engine applies to
// its own subscribers, for the same reason.
func (c *Client) pushEvent(ev engine.Event) {
	select {
	case c.events <- ev:
	default:
		select {
		case <-c.events:
		default:
		}
		select {
		case c.events <- ev:
		default:
		}
	}
}

// failAllPending wakes every waiting caller with a failure response.
func (c *Client) failAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for seq, ch := range c.pending {
		ch <- &Response{Seq: seq, Err: err.Error()}
		delete(c.pending, seq)
	}
}

// call sends a request and waits for the reply carrying its sequence
// number.
func (c *Client) call(req *Request) (*Response, error) {
	c.mu.Lock()
	c.seq++
	req.Seq = c.seq
	ch := make(chan *Response, 1)
	c.pending[req.Seq] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err := c.enc.Encode(message{Kind: kindCall, Req: req})
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, req.Seq)
		c.mu.Unlock()
		return nil, fmt.Errorf("send %s: %w", req.Method, err)
	}

	select {
	case resp := <-ch:
		if resp.Err != "" {
			return nil, fmt.Errorf("%s: %s", req.Method, resp.Err)
		}
		return resp, nil
	case <-c.closed:
		return nil, fmt.Errorf("%s: client closed", req.Method)
	case <-time.After(callTimeout):
		// Give up rather than block a UI forever on a wedged daemon.
		c.mu.Lock()
		delete(c.pending, req.Seq)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: timed out after %s", req.Method, callTimeout)
	}
}

func (c *Client) Ping() error {
	_, err := c.call(&Request{Method: MethodPing})
	return err
}

func (c *Client) AddMagnet(uri string, opts engine.AddOpts) (engine.TorrentID, error) {
	resp, err := c.call(&Request{Method: MethodAddMagnet, Source: uri, Opts: opts})
	if err != nil {
		return "", err
	}
	return engine.TorrentID(resp.ID), nil
}

func (c *Client) AddTorrentFile(path string, opts engine.AddOpts) (engine.TorrentID, error) {
	resp, err := c.call(&Request{Method: MethodAddTorrentFile, Source: path, Opts: opts})
	if err != nil {
		return "", err
	}
	return engine.TorrentID(resp.ID), nil
}

// AddBatch adds many sources in one call, returning the ids that worked
// and a message for each that did not. A batch can half succeed, and the
// caller needs both halves.
func (c *Client) AddBatch(sources []string, opts engine.AddOpts) (ids []engine.TorrentID, errs []string, err error) {
	resp, err := c.call(&Request{Method: MethodAddBatch, Sources: sources, Opts: opts})
	if err != nil {
		return nil, nil, err
	}
	for _, id := range resp.IDs {
		ids = append(ids, engine.TorrentID(id))
	}
	return ids, resp.Errs, nil
}

func (c *Client) Remove(id engine.TorrentID, deleteData bool) error {
	_, err := c.call(&Request{Method: MethodRemove, ID: string(id), DeleteData: deleteData})
	return err
}

// PurgeData deletes a torrent's data, keeping the torrent itself.
func (c *Client) PurgeData(id engine.TorrentID) error {
	_, err := c.call(&Request{Method: MethodPurgeData, ID: string(id)})
	return err
}

func (c *Client) SetPaused(id engine.TorrentID, paused bool) error {
	_, err := c.call(&Request{Method: MethodSetPaused, ID: string(id), Paused: paused})
	return err
}

func (c *Client) ForceRecheck(id engine.TorrentID) error {
	_, err := c.call(&Request{Method: MethodForceRecheck, ID: string(id)})
	return err
}

func (c *Client) SetFilePriority(id engine.TorrentID, fileIndex int, prio engine.Priority) error {
	_, err := c.call(&Request{Method: MethodSetFilePriority, ID: string(id), FileIndex: fileIndex, Priority: prio})
	return err
}

func (c *Client) SetGlobalUploadLimit(bps int64) error {
	_, err := c.call(&Request{Method: MethodSetGlobalUploadLimit, UploadBps: bps})
	return err
}

func (c *Client) SetGlobalDownloadLimit(bps int64) error {
	_, err := c.call(&Request{Method: MethodSetGlobalDownloadLimit, DownloadBps: bps})
	return err
}

func (c *Client) SetTorrentRateLimit(id engine.TorrentID, uploadBps, downloadBps int64) error {
	_, err := c.call(&Request{Method: MethodSetTorrentRateLimit, ID: string(id), UploadBps: uploadBps, DownloadBps: downloadBps})
	return err
}

func (c *Client) MoveStorage(id engine.TorrentID, newDir string) error {
	_, err := c.call(&Request{Method: MethodMoveStorage, ID: string(id), NewDir: newDir})
	return err
}

func (c *Client) List() ([]engine.TorrentSnapshot, error) {
	resp, err := c.call(&Request{Method: MethodList})
	if err != nil {
		return nil, err
	}
	return resp.Snapshot, nil
}

// PreviewURL asks the daemon to make one file streamable and return the
// loopback URL a player can open. The daemon resumes the torrent and
// raises the file's priority as a side effect: a stream of paused or
// unwanted data would only error out.
func (c *Client) PreviewURL(id engine.TorrentID, fileIndex int) (string, error) {
	resp, err := c.call(&Request{Method: MethodPreviewURL, ID: string(id), FileIndex: fileIndex})
	if err != nil {
		return "", err
	}
	if resp.URL == "" {
		return "", fmt.Errorf("empty preview url response")
	}
	return resp.URL, nil
}

func (c *Client) Detail(id engine.TorrentID) (engine.TorrentDetail, error) {
	resp, err := c.call(&Request{Method: MethodDetail, ID: string(id)})
	if err != nil {
		return engine.TorrentDetail{}, err
	}
	if resp.Detail == nil {
		return engine.TorrentDetail{}, fmt.Errorf("empty detail response")
	}
	return *resp.Detail, nil
}
