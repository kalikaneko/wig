package crudlog

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

func doFollow(ctx context.Context, src LogSource, dst LogSink) error {
	replState.Set(0)
	start := dst.LatestSequence()

	// Start a subscription with the snapshot as a reference.
	restartFromSnapshot := true

retry:
	log.Printf("follow starts from local sequence %s", start)
	sub, err := src.Subscribe(ctx, start+1)
	if errors.Is(err, ErrHorizon) && restartFromSnapshot {
		// Can't recover from 'start', grab a snapshot.
		log.Printf("index %s is past the remote horizon, grabbing snapshot", start)
		snapshotCounter.Inc()
		snap, serr := src.Snapshot(ctx)
		if serr != nil {
			return fmt.Errorf("Snapshot() error: %w", serr)
		}
		if err := dst.LoadSnapshot(snap); err != nil {
			return fmt.Errorf("loading snapshot %s: %w", snap.Seq(), err)
		}
		log.Printf("loaded snapshot %s", snap.Seq())
		start = snap.Seq()

		// Try again.
		restartFromSnapshot = false
		goto retry
	}
	if err != nil {
		return fmt.Errorf("subscription error: %w", err)
	}
	defer sub.Close()

	ch := sub.Notify()
	replState.Set(1)
	for {
		select {
		case op := <-ch:
			if op == nil {
				return nil
			}
			latestSequence.Set(float64(op.Seq()))
			if err := dst.Apply(op, true); err != nil {
				return fmt.Errorf("sequence %s: %w", op.Seq(), err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func Follow(ctx context.Context, src LogSource, dst LogSink) error {
	// Outer loop around doFollow that catches transport errors on
	// Subscribe() and restarts the process from where it left
	// off. Transport errors result in doFollow() returning nil,
	// any other error is considered permanent.
	for {
		err := doFollow(ctx, src, dst)
		if err != nil {
			return err
		}
	}
}

var (
	replState = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "async_repl_up",
			Help: "Status of the asynchronous replication process.",
		})
	snapshotCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "async_repl_snapshots_total",
			Help: "Total number of Snapshot() calls.",
		})
	latestSequence = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "async_repl_sequence",
			Help: "Last sequence number seen.",
		})
)

func init() {
	prometheus.MustRegister(
		replState,
		snapshotCounter,
		latestSequence,
	)
}
