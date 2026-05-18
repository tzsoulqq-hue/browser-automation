package camoufox

import _ "embed"

//go:embed scripts/server.py
var serverScript string

//go:embed scripts/worker.py
var workerScript string

type scripts struct {
	server string
	worker string
}

func defaultScripts() scripts {
	return scripts{
		server: serverScript,
		worker: workerScript,
	}
}
