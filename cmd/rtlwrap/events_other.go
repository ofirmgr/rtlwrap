//go:build !darwin || !cgo

package main

func runWithInputEvents(work func() error) error { return work() }
