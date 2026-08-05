package main

import "github.com/lestex/torrnado/internal/config"

// configPathFlag is set by the root command's --config flag.
var configPathFlag string

// loadConfig resolves --config (or the XDG default) and loads+validates
// it, returning the path it read (or would read) alongside the Config.
//
// The path is returned as well as the config so the daemon can say which
// file it used -- a config that was never found looks exactly like one
// that was found and had nothing to say.
func loadConfig() (config.Config, string, error) {
	path := configPathFlag
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return config.Config{}, "", err
		}
		path = p
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, path, nil
}
