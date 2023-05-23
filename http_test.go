// Copyright (c) 2015 Klaus Post, 2023 Eik Madsen, released under MIT License. See LICENSE file.

package shutdown

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

var (
	m = New()
)

// This example creates a custom function handler
// and wraps the handler, so all request will
// finish before shutdown is started.
//
// If requests take too long to finish (see the shutdown will proceed
// and clients will be disconnected when the server shuts down.
// To modify the timeout use m.SetTimeoutN(m.PreShutdown, duration)
func ExampleWrapHandlerFunc() {
	// Set a custom timeout, if the 5 second default doesn't fit your needs.
	m.SetTimeoutN(m.StagePS, time.Second*30)
	// Catch OS signals
	m.OnSignal(0, os.Interrupt, syscall.SIGTERM)

	// Example handler function
	fn := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
	}

	// Wrap the handler function
	http.HandleFunc("/", m.WrapHandlerFunc(fn))

	// Start the server
	http.ListenAndServe(":8080", nil)
}

// This example creates a fileserver
// and wraps the handler, so all request will
// finish before shutdown is started.
//
// If requests take too long to finish the shutdown will proceed
// and clients will be disconnected when the server shuts down.
// To modify the timeout use m.SetTimeoutN(m.PreShutdown, duration)
func ExampleWrapHandler() {
	// Set a custom timeout, if the 5 second default doesn't fit your needs.
	m.SetTimeoutN(m.StagePS, time.Second*30)
	// Catch OS signals
	m.OnSignal(0, os.Interrupt, syscall.SIGTERM)

	// Create a fileserver handler
	fh := http.FileServer(http.Dir("/examples"))

	// Wrap the handler function
	http.Handle("/", m.WrapHandler(fh))

	// Start the server
	http.ListenAndServe(":8080", nil)
}

func TestWrapHandlerBasic(t *testing.T) {
	m := New()
	defer close(startTimer(m, t))
	var finished = false
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finished = true
	})

	wrapped := m.WrapHandler(fn)
	res := httptest.NewRecorder()
	req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
	wrapped.ServeHTTP(res, req)
	if res.Code == http.StatusServiceUnavailable {
		t.Fatal("Expected result code NOT to be", http.StatusServiceUnavailable, "got", res.Code)
	}
	if !finished {
		t.Fatal("Handler was not executed")
	}

	m.Shutdown()
	finished = false
	res = httptest.NewRecorder()
	wrapped.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatal("Expected result code to be", http.StatusServiceUnavailable, " got", res.Code)
	}
	if finished {
		t.Fatal("Unexpected execution of funtion")
	}
}

func TestWrapHandlerFuncBasic(t *testing.T) {
	m := New()
	defer close(startTimer(m, t))
	var finished = false
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finished = true
	})

	wrapped := m.WrapHandlerFunc(fn)
	res := httptest.NewRecorder()
	req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
	wrapped(res, req)
	if res.Code == http.StatusServiceUnavailable {
		t.Fatal("Expected result code NOT to be", http.StatusServiceUnavailable, "got", res.Code)
	}
	if !finished {
		t.Fatal("Handler was not executed")
	}

	m.Shutdown()
	finished = false
	res = httptest.NewRecorder()
	wrapped(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatal("Expected result code to be", http.StatusServiceUnavailable, " got", res.Code)
	}
	if finished {
		t.Fatal("Unexpected execution of funtion")
	}
}

// Test if panics locks shutdown.
func TestWrapHandlerPanic(t *testing.T) {
	m := New()
	m.SetTimeout(time.Second)
	defer close(startTimer(m, t))
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := m.WrapHandler(fn)
	res := httptest.NewRecorder()
	req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
	func() {
		defer func() {
			recover()
		}()
		wrapped.ServeHTTP(res, req)
	}()

	// There should be no locks held, so it should finish immediately
	tn := time.Now()
	m.Shutdown()
	dur := time.Since(tn)
	if dur > time.Millisecond*500 {
		t.Fatalf("timeout time was unexpected:%v", time.Since(tn))
	}
}

// Test if panics locks shutdown.
func TestWrapHandlerFuncPanic(t *testing.T) {
	m := New()
	m.SetTimeout(time.Millisecond * 200)
	defer close(startTimer(m, t))
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := m.WrapHandlerFunc(fn)
	res := httptest.NewRecorder()
	req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
	func() {
		defer func() {
			recover()
		}()
		wrapped(res, req)
	}()

	// There should be no locks held, so it should finish immediately
	tn := time.Now()
	m.Shutdown()
	dur := time.Since(tn)
	if dur > time.Millisecond*100 {
		t.Fatalf("timeout time was unexpected:%v", time.Since(tn))
	}
}

// Tests that shutdown doesn't complete until handler function has returned
func TestWrapHandlerOrder(t *testing.T) {
	m := New()
	defer close(startTimer(m, t))
	var finished = make(chan bool)
	var wait = make(chan bool)
	var waiting = make(chan bool)
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(waiting)
		<-wait
	})

	wrapped := m.WrapHandler(fn)

	go func() {
		res := httptest.NewRecorder()
		req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
		wrapped.ServeHTTP(res, req)
		close(finished)
	}()

	release := time.After(time.Millisecond * 100)
	completed := make(chan bool)
	testOK := make(chan bool)
	go func() {
		select {
		case <-release:
			select {
			case <-finished:
				panic("Shutdown was already finished")
			case <-completed:
				panic("Shutdown had already completed")
			default:
			}
			close(wait)
			close(testOK)
		}
	}()
	<-waiting
	tn := time.Now()
	m.Shutdown()
	dur := time.Since(tn)
	if dur > time.Millisecond*400 {
		t.Fatalf("timeout time was unexpected:%v", time.Since(tn))
	}
	close(completed)
	// We should make sure the release has run before exiting
	<-testOK

	time.Sleep(time.Millisecond * 50)
	select {
	case <-finished:
	default:
		t.Fatal("Function had not finished")
	}
}

// Tests that shutdown doesn't complete until handler function has returned
func TestWrapHandlerFuncOrder(t *testing.T) {
	m := New()
	defer close(startTimer(m, t))
	var finished = make(chan bool)
	var wait = make(chan bool)
	var waiting = make(chan bool)
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		close(waiting)
		<-wait
	})

	wrapped := m.WrapHandlerFunc(fn)

	go func() {
		res := httptest.NewRecorder()
		req, _ := http.NewRequest("", "", bytes.NewBufferString(""))
		wrapped(res, req)
	}()

	release := time.After(time.Millisecond * 100)
	completed := make(chan bool)
	testOK := make(chan bool)
	go func() {
		select {
		case <-release:
			select {
			case <-finished:
				panic("Shutdown was already finished")
			case <-completed:
				panic("Shutdown had already completed")
			default:
			}
			close(wait)
			close(testOK)
		}
	}()
	<-waiting
	tn := time.Now()
	m.Shutdown()
	dur := time.Since(tn)
	if dur > time.Millisecond*400 {
		t.Fatalf("timeout time was unexpected:%v", time.Since(tn))
	}
	close(completed)
	// We should make sure the release has run before exiting
	<-testOK

	select {
	case <-finished:
	default:
		t.Fatal("Function had not finished")
	}
}
