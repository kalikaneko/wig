package peerdb

import (
	"context"
	"errors"
	"fmt"
	"log"
)

func Follow(ctx context.Context, src Log, dst LogReceiver) error {
	start := dst.LatestSequence()

	// Start a subscription with the snapshot as a reference.
	restartFromSnapshot := true

retry:
	log.Printf("follow starts from local sequence %s", start)
	sub, err := src.Subscribe(ctx, start)
	if errors.Is(err, ErrHorizon) && restartFromSnapshot {
		// Can't recover from 'start', grab a snapshot.
		log.Printf("index %s is past the remote horizon, grabbing snapshot", start)
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
	for {
		select {
		case op := <-ch:
			if err := dst.Apply(op); err != nil {
				return fmt.Errorf("sequence %s: %w", op.Seq, err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
