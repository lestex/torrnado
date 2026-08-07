//go:build !linux && !darwin

package engine

// diskUsage is unimplemented on this platform; the global stats bar will
// simply show no disk-space figures.
func diskUsage(path string) (free, total int64, err error) {
	return 0, 0, nil
}
