/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"errors"
	"testing"
)

func TestBlockchainExplorerHelpersRequireDatabase(t *testing.T) {
	originalPool := pool
	pool = nil
	t.Cleanup(func() {
		pool = originalPool
	})

	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "list blockchain records",
			call: func() error {
				_, err := ListBlockchainRecords(ctx, 10)
				return err
			},
		},
		{
			name: "list entity blockchain records",
			call: func() error {
				_, err := ListBlockchainRecordsForEntity(ctx, "course", "entity-id")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrDatabaseConnectionNotInitialized) {
				t.Fatalf("expected %v, got %v", ErrDatabaseConnectionNotInitialized, err)
			}
		})
	}
}
