//go:build ignore
// +build ignore

package main

// This example shows a server that has logging in a separate goroutine
//
// When the server is shut down (via ctrl+c for instance), it will flush
// all queued log entries and close the file.
//
// To execute, use 'go run server-channel.go'

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"syscall"

	shutdownp "github.com/Vivino/go-shutdown"
)

var (
	shutdown = shutdownp.New()
)

func main() {
	// Make shutdown catch Ctrl+c and system terminate
	shutdown.OnSignal(0, os.Interrupt, syscall.SIGTERM)

	logStream = make(chan string, 100)

	// start a logger receiver
	go logger()

	// Start a webserver
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		l := shutdown.Lock()
		if l != nil {
			logStream <- req.URL.String() + "\n"
			l()
		} else {
			log.Println("Already shutting down")
		}
	})
	fmt.Println("Starting server. Press Ctrl+c to initiate shutdown")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

var logStream chan string

func logger() {
	logFile, _ := os.Create("log.txt")

	// Get a nofification when we are at third stage of shutting down
	exit := shutdown.Third()
	for {
		select {
		case v := <-exit.Notify():
			log.Println("Flushing log...")
			finished := false
			for !finished {
				select {
				case m := <-logStream:
					_, _ = io.WriteString(logFile, m)
				default:
					finished = true
				}
			}
			log.Println("Closing log...")
			logFile.Close()
			// Signal we are done
			close(v)
			return
		case v := <-logStream:
			_, _ = io.WriteString(logFile, v)
		}
	}
}
