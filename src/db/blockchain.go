/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const blockchainAdvisoryLockKey int64 = 8602002

const (
	BlockchainEventGenesis  = "genesis"
	BlockchainEntitySystem  = "system"
	BlockchainEntityGenesis = "genesis"
)

// ChainBlock represents a stored blockchain block.
type ChainBlock struct {
	ID                string
	Height            int64
	PreviousBlockHash *string
	BlockHash         string
	EventType         string
	EventHash         string
	ActorUserID       *string
	OccurredAt        time.Time
	CreatedAt         time.Time
}

// ChainEvent represents a stored blockchain event.
type ChainEvent struct {
	ID          string
	BlockID     string
	EventType   string
	EntityType  string
	EntityID    *string
	PayloadJSON []byte
	PayloadHash string
	ActorUserID *string
	OccurredAt  time.Time
	CreatedAt   time.Time
}

// BlockchainRecord represents a block and its linked event.
type BlockchainRecord struct {
	Block ChainBlock
	Event ChainEvent
}

// BlockchainEventPayload is the fixed envelope stored in chain_events.payload_json.
type BlockchainEventPayload struct {
	EventType   string          `json:"event_type"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id,omitempty"`
	ActorUserID string          `json:"actor_user_id,omitempty"`
	OccurredAt  string          `json:"occurred_at"`
	Data        json.RawMessage `json:"data"`
}

// AppendBlockchainEventInput contains the data needed to append a new event.
type AppendBlockchainEventInput struct {
	EventType   string
	EntityType  string
	EntityID    string
	ActorUserID string
	OccurredAt  time.Time
	Data        any
}

// BlockchainVerificationResult summarizes full-chain verification.
type BlockchainVerificationResult struct {
	Valid         bool
	CheckedBlocks int
	HeadHeight    int64
	HeadHash      string
	Failure       string
}

// BlockchainRecordSummary is a denormalized record for explorer and audit views.
type BlockchainRecordSummary struct {
	BlockID           string
	BlockHeight       int64
	PreviousBlockHash *string
	BlockHash         string
	EventID           string
	EventType         string
	EventHash         string
	EntityType        string
	EntityID          *string
	PayloadJSON       []byte
	PayloadHash       string
	ActorUserID       *string
	ActorDisplayName  *string
	ActorUsername     *string
	OccurredAt        time.Time
	CreatedAt         time.Time
}

type chainEventHashDocument struct {
	EventType   string `json:"event_type"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id,omitempty"`
	ActorUserID string `json:"actor_user_id,omitempty"`
	OccurredAt  string `json:"occurred_at"`
	PayloadHash string `json:"payload_hash"`
}

type chainBlockHashDocument struct {
	Height            int64  `json:"height"`
	PreviousBlockHash string `json:"previous_block_hash,omitempty"`
	EventHash         string `json:"event_hash"`
	EventType         string `json:"event_type"`
	ActorUserID       string `json:"actor_user_id,omitempty"`
	OccurredAt        string `json:"occurred_at"`
}

// EnsureBlockchainReady makes sure the genesis block exists.
func EnsureBlockchainReady(ctx context.Context) error {
	if _, err := EnsureGenesisBlock(ctx); err != nil {
		return fmt.Errorf("failed to ensure blockchain genesis: %w", err)
	}

	return nil
}

// EnsureGenesisBlock creates the genesis block if the chain is empty.
func EnsureGenesisBlock(ctx context.Context) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin blockchain transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback blockchain transaction", "error", rollbackErr)
		}
	}()

	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return false, err
	}

	head, err := getBlockchainHeadTx(ctx, tx)
	if err != nil {
		return false, err
	}

	if head != nil {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("failed to commit blockchain transaction: %w", err)
		}

		return false, nil
	}

	if _, err := createGenesisBlockTx(ctx, tx, time.Now().UTC()); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit blockchain transaction: %w", err)
	}

	return true, nil
}

// CreateGenesisBlock creates the genesis block on an empty chain.
func CreateGenesisBlock(ctx context.Context) (*BlockchainRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin blockchain transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback blockchain transaction", "error", rollbackErr)
		}
	}()

	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	head, err := getBlockchainHeadTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	if head != nil {
		return nil, ErrBlockchainAlreadyInitialized
	}

	record, err := createGenesisBlockTx(ctx, tx, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit blockchain transaction: %w", err)
	}

	return record, nil
}

// GetBlockchainHead returns the latest block, or nil when the chain is empty.
func GetBlockchainHead(ctx context.Context) (*ChainBlock, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getBlockchainHeadQuerier(ctx, pool)
}

// AppendBlockchainEvent appends a new event and block in one transaction.
func AppendBlockchainEvent(ctx context.Context, input AppendBlockchainEventInput) (*BlockchainRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin blockchain transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback blockchain transaction", "error", rollbackErr)
		}
	}()

	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	record, err := appendBlockchainEventTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit blockchain transaction: %w", err)
	}

	return record, nil
}

func appendBlockchainEventTx(ctx context.Context, tx pgx.Tx, input AppendBlockchainEventInput) (*BlockchainRecord, error) {
	built, err := buildBlockchainPayload(input)
	if err != nil {
		return nil, err
	}

	head, err := getBlockchainHeadTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	if head == nil {
		headRecord, err := createGenesisBlockTx(ctx, tx, built.OccurredAt)
		if err != nil {
			return nil, err
		}

		head = &headRecord.Block
	}

	height := head.Height + 1
	blockHash, err := computeBlockHash(height, head.BlockHash, built.EventHash, built.EventType, built.ActorUserID, built.OccurredAt)
	if err != nil {
		return nil, err
	}

	record, err := insertBlockchainRecordTx(ctx, tx, insertBlockchainRecordInput{
		Height:            height,
		PreviousBlockHash: &head.BlockHash,
		BlockHash:         blockHash,
		EventType:         built.EventType,
		EventHash:         built.EventHash,
		EntityType:        built.EntityType,
		EntityID:          built.EntityID,
		PayloadJSON:       built.PayloadJSON,
		PayloadHash:       built.PayloadHash,
		ActorUserID:       built.ActorUserID,
		OccurredAt:        built.OccurredAt,
	})
	if err != nil {
		return nil, err
	}

	return record, nil
}

// VerifyBlockchain verifies the entire chain from genesis to head.
func VerifyBlockchain(ctx context.Context) (*BlockchainVerificationResult, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT
			b.id,
			b.height,
			b.previous_block_hash,
			b.block_hash,
			b.event_type,
			b.event_hash,
			b.actor_user_id,
			b.occurred_at,
			b.created_at,
			e.id,
			e.block_id,
			e.event_type,
			e.entity_type,
			e.entity_id,
			e.payload_json::text,
			e.payload_hash,
			e.actor_user_id,
			e.occurred_at,
			e.created_at
		FROM chain_blocks b
		JOIN chain_events e ON e.block_id = b.id
		ORDER BY b.height ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query blockchain records: %w", err)
	}

	defer rows.Close()

	records := make([]BlockchainRecord, 0)

	for rows.Next() {
		var record BlockchainRecord

		if err := rows.Scan(
			&record.Block.ID,
			&record.Block.Height,
			&record.Block.PreviousBlockHash,
			&record.Block.BlockHash,
			&record.Block.EventType,
			&record.Block.EventHash,
			&record.Block.ActorUserID,
			&record.Block.OccurredAt,
			&record.Block.CreatedAt,
			&record.Event.ID,
			&record.Event.BlockID,
			&record.Event.EventType,
			&record.Event.EntityType,
			&record.Event.EntityID,
			&record.Event.PayloadJSON,
			&record.Event.PayloadHash,
			&record.Event.ActorUserID,
			&record.Event.OccurredAt,
			&record.Event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan blockchain record: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blockchain records: %w", err)
	}

	return verifyBlockchainRecords(records)
}

// ListBlockchainRecords returns recent blockchain records for explorer pages.
func ListBlockchainRecords(ctx context.Context, limit int) ([]BlockchainRecordSummary, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	if limit <= 0 {
		limit = 100
	}

	return listBlockchainRecords(ctx, `
		SELECT
			b.id,
			b.height,
			b.previous_block_hash,
			b.block_hash,
			e.id,
			e.event_type,
			b.event_hash,
			e.entity_type,
			e.entity_id,
			e.payload_json::text,
			e.payload_hash,
			e.actor_user_id,
			u.display_name,
			u.username,
			b.occurred_at,
			b.created_at
		FROM chain_blocks b
		JOIN chain_events e ON e.block_id = b.id
		LEFT JOIN users u ON u.id = e.actor_user_id
		ORDER BY b.height DESC
		LIMIT $1
	`, limit)
}

// ListBlockchainRecordsForEntity returns blockchain records linked to one entity.
func ListBlockchainRecordsForEntity(ctx context.Context, entityType string, entityID string) ([]BlockchainRecordSummary, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return listBlockchainRecords(ctx, `
		SELECT
			b.id,
			b.height,
			b.previous_block_hash,
			b.block_hash,
			e.id,
			e.event_type,
			b.event_hash,
			e.entity_type,
			e.entity_id,
			e.payload_json::text,
			e.payload_hash,
			e.actor_user_id,
			u.display_name,
			u.username,
			b.occurred_at,
			b.created_at
		FROM chain_blocks b
		JOIN chain_events e ON e.block_id = b.id
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.entity_type = $1
		  AND e.entity_id = $2
		ORDER BY b.height DESC
	`, entityType, entityID)
}

type insertBlockchainRecordInput struct {
	Height            int64
	PreviousBlockHash *string
	BlockHash         string
	EventType         string
	EventHash         string
	EntityType        string
	EntityID          *string
	PayloadJSON       []byte
	PayloadHash       string
	ActorUserID       *string
	OccurredAt        time.Time
}

type builtBlockchainPayload struct {
	EventType   string
	EntityType  string
	EntityID    *string
	ActorUserID *string
	OccurredAt  time.Time
	PayloadJSON []byte
	PayloadHash string
	EventHash   string
}

func acquireBlockchainLockTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, blockchainAdvisoryLockKey); err != nil {
		return fmt.Errorf("failed to acquire blockchain advisory lock: %w", err)
	}

	return nil
}

func createGenesisBlockTx(ctx context.Context, tx pgx.Tx, occurredAt time.Time) (*BlockchainRecord, error) {
	occurredAt = normalizeBlockchainTime(occurredAt)

	payload := BlockchainEventPayload{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: formatBlockchainTime(occurredAt),
		Data:       json.RawMessage(`{"message":"taalam blockchain genesis block"}`),
	}

	payloadJSON, err := marshalCanonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal genesis payload: %w", err)
	}
	payloadJSON, err = canonicalizeBlockchainPayloadJSON(payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize genesis payload: %w", err)
	}

	payloadHash := hashBytesSHA256Hex(payloadJSON)
	eventHash, err := computeEventHash(
		BlockchainEventGenesis,
		BlockchainEntitySystem,
		stringPointer(BlockchainEntityGenesis),
		nil,
		occurredAt,
		payloadHash,
	)
	if err != nil {
		return nil, err
	}

	blockHash, err := computeBlockHash(0, "", eventHash, BlockchainEventGenesis, nil, occurredAt)
	if err != nil {
		return nil, err
	}

	return insertBlockchainRecordTx(ctx, tx, insertBlockchainRecordInput{
		Height:            0,
		PreviousBlockHash: nil,
		BlockHash:         blockHash,
		EventType:         BlockchainEventGenesis,
		EventHash:         eventHash,
		EntityType:        BlockchainEntitySystem,
		EntityID:          stringPointer(BlockchainEntityGenesis),
		PayloadJSON:       payloadJSON,
		PayloadHash:       payloadHash,
		ActorUserID:       nil,
		OccurredAt:        occurredAt.UTC(),
	})
}

func buildBlockchainPayload(input AppendBlockchainEventInput) (*builtBlockchainPayload, error) {
	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		return nil, ErrBlockchainEventTypeRequired
	}

	entityType := strings.TrimSpace(input.EntityType)
	if entityType == "" {
		return nil, ErrBlockchainEntityTypeRequired
	}

	entityID := trimmedOptionalString(input.EntityID)
	actorUserID, err := normalizeOptionalUUIDString(input.ActorUserID)
	if err != nil {
		return nil, err
	}

	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	occurredAt = normalizeBlockchainTime(occurredAt)

	dataJSON, err := marshalCanonicalJSON(input.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal blockchain event data: %w", err)
	}

	payload := BlockchainEventPayload{
		EventType:  eventType,
		EntityType: entityType,
		OccurredAt: formatBlockchainTime(occurredAt),
		Data:       json.RawMessage(dataJSON),
	}
	if entityID != nil {
		payload.EntityID = *entityID
	}
	if actorUserID != nil {
		payload.ActorUserID = *actorUserID
	}

	payloadJSON, err := marshalCanonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal blockchain payload: %w", err)
	}
	payloadJSON, err = canonicalizeBlockchainPayloadJSON(payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize blockchain payload: %w", err)
	}

	payloadHash := hashBytesSHA256Hex(payloadJSON)
	eventHash, err := computeEventHash(eventType, entityType, entityID, actorUserID, occurredAt, payloadHash)
	if err != nil {
		return nil, err
	}

	return &builtBlockchainPayload{
		EventType:   eventType,
		EntityType:  entityType,
		EntityID:    entityID,
		ActorUserID: actorUserID,
		OccurredAt:  occurredAt,
		PayloadJSON: payloadJSON,
		PayloadHash: payloadHash,
		EventHash:   eventHash,
	}, nil
}

func computeEventHash(eventType string, entityType string, entityID *string, actorUserID *string, occurredAt time.Time, payloadHash string) (string, error) {
	doc := chainEventHashDocument{
		EventType:   eventType,
		EntityType:  entityType,
		OccurredAt:  formatBlockchainTime(occurredAt),
		PayloadHash: payloadHash,
	}
	if entityID != nil {
		doc.EntityID = *entityID
	}
	if actorUserID != nil {
		doc.ActorUserID = *actorUserID
	}

	data, err := marshalCanonicalJSON(doc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal blockchain event hash document: %w", err)
	}

	return hashBytesSHA256Hex(data), nil
}

func computeBlockHash(height int64, previousBlockHash string, eventHash string, eventType string, actorUserID *string, occurredAt time.Time) (string, error) {
	doc := chainBlockHashDocument{
		Height:     height,
		EventHash:  eventHash,
		EventType:  eventType,
		OccurredAt: formatBlockchainTime(occurredAt),
	}
	if previousBlockHash != "" {
		doc.PreviousBlockHash = previousBlockHash
	}
	if actorUserID != nil {
		doc.ActorUserID = *actorUserID
	}

	data, err := marshalCanonicalJSON(doc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal blockchain block hash document: %w", err)
	}

	return hashBytesSHA256Hex(data), nil
}

func insertBlockchainRecordTx(ctx context.Context, tx pgx.Tx, input insertBlockchainRecordInput) (*BlockchainRecord, error) {
	var block ChainBlock
	err := tx.QueryRow(ctx, `
		INSERT INTO chain_blocks (
			height,
			previous_block_hash,
			block_hash,
			event_type,
			event_hash,
			actor_user_id,
			occurred_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, height, previous_block_hash, block_hash, event_type, event_hash, actor_user_id, occurred_at, created_at
	`, input.Height, input.PreviousBlockHash, input.BlockHash, input.EventType, input.EventHash, input.ActorUserID, input.OccurredAt).Scan(
		&block.ID,
		&block.Height,
		&block.PreviousBlockHash,
		&block.BlockHash,
		&block.EventType,
		&block.EventHash,
		&block.ActorUserID,
		&block.OccurredAt,
		&block.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert blockchain block: %w", err)
	}

	var event ChainEvent
	err = tx.QueryRow(ctx, `
		INSERT INTO chain_events (
			block_id,
			event_type,
			entity_type,
			entity_id,
			payload_json,
			payload_hash,
			actor_user_id,
			occurred_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING id, block_id, event_type, entity_type, entity_id, payload_json::text, payload_hash, actor_user_id, occurred_at, created_at
	`, block.ID, input.EventType, input.EntityType, input.EntityID, string(input.PayloadJSON), input.PayloadHash, input.ActorUserID, input.OccurredAt).Scan(
		&event.ID,
		&event.BlockID,
		&event.EventType,
		&event.EntityType,
		&event.EntityID,
		&event.PayloadJSON,
		&event.PayloadHash,
		&event.ActorUserID,
		&event.OccurredAt,
		&event.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert blockchain event: %w", err)
	}

	return &BlockchainRecord{Block: block, Event: event}, nil
}

func getBlockchainHeadTx(ctx context.Context, tx pgx.Tx) (*ChainBlock, error) {
	return getBlockchainHeadQuerier(ctx, tx)
}

type blockchainHeadQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getBlockchainHeadQuerier(ctx context.Context, querier blockchainHeadQuerier) (*ChainBlock, error) {
	var block ChainBlock
	err := querier.QueryRow(ctx, `
		SELECT id, height, previous_block_hash, block_hash, event_type, event_hash, actor_user_id, occurred_at, created_at
		FROM chain_blocks
		ORDER BY height DESC
		LIMIT 1
	`).Scan(
		&block.ID,
		&block.Height,
		&block.PreviousBlockHash,
		&block.BlockHash,
		&block.EventType,
		&block.EventHash,
		&block.ActorUserID,
		&block.OccurredAt,
		&block.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load blockchain head: %w", err)
	}

	return &block, nil
}

func listBlockchainRecords(ctx context.Context, query string, args ...any) ([]BlockchainRecordSummary, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query blockchain records: %w", err)
	}
	defer rows.Close()

	records := make([]BlockchainRecordSummary, 0)
	for rows.Next() {
		var record BlockchainRecordSummary
		if err := rows.Scan(
			&record.BlockID,
			&record.BlockHeight,
			&record.PreviousBlockHash,
			&record.BlockHash,
			&record.EventID,
			&record.EventType,
			&record.EventHash,
			&record.EntityType,
			&record.EntityID,
			&record.PayloadJSON,
			&record.PayloadHash,
			&record.ActorUserID,
			&record.ActorDisplayName,
			&record.ActorUsername,
			&record.OccurredAt,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan blockchain record summary: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blockchain record summaries: %w", err)
	}

	return records, nil
}

func verifyBlockchainRecords(records []BlockchainRecord) (*BlockchainVerificationResult, error) {
	result := &BlockchainVerificationResult{Valid: true, HeadHeight: -1}
	var previousBlock *ChainBlock

	for _, record := range records {
		result.CheckedBlocks++
		result.HeadHeight = record.Block.Height
		result.HeadHash = record.Block.BlockHash

		if record.Event.BlockID != record.Block.ID {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d event link mismatch", record.Block.Height)

			return result, nil
		}

		if record.Event.EventType != record.Block.EventType {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d event type mismatch", record.Block.Height)

			return result, nil
		}

		canonicalPayloadJSON, err := canonicalizeBlockchainPayloadJSON(record.Event.PayloadJSON)
		if err != nil {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d payload JSON invalid", record.Block.Height)

			return result, nil
		}

		payloadHashMatches, err := matchesStoredBlockchainPayloadHash(record.Event.PayloadJSON, canonicalPayloadJSON, record.Event.PayloadHash)
		if err != nil {
			return nil, err
		}
		if !payloadHashMatches {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d payload hash mismatch", record.Block.Height)

			return result, nil
		}

		payload, err := parseBlockchainPayloadJSON(record.Event.PayloadJSON)
		if err != nil {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d payload JSON invalid", record.Block.Height)

			return result, nil
		}

		hashOccurredAt, err := parseBlockchainTime(payload.OccurredAt)
		if err != nil {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d payload JSON invalid", record.Block.Height)

			return result, nil
		}

		eventHash, err := computeEventHash(
			record.Event.EventType,
			record.Event.EntityType,
			record.Event.EntityID,
			record.Event.ActorUserID,
			hashOccurredAt,
			record.Event.PayloadHash,
		)
		if err != nil {
			return nil, err
		}

		eventHashMatches := eventHash == record.Block.EventHash
		if !eventHashMatches && !(previousBlock == nil && isExpectedGenesisRecord(record, hashOccurredAt, canonicalPayloadJSON)) {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d event hash mismatch", record.Block.Height)

			return result, nil
		}

		if previousBlock == nil {
			if record.Block.Height != 0 {
				result.Valid = false
				result.Failure = "genesis block must have height 0"

				return result, nil
			}

			if record.Block.PreviousBlockHash != nil {
				result.Valid = false
				result.Failure = "genesis block must not have a previous block hash"

				return result, nil
			}
		} else {
			if record.Block.Height != previousBlock.Height+1 {
				result.Valid = false
				result.Failure = fmt.Sprintf("block %d height is not sequential", record.Block.Height)

				return result, nil
			}

			if record.Block.PreviousBlockHash == nil || *record.Block.PreviousBlockHash != previousBlock.BlockHash {
				result.Valid = false
				result.Failure = fmt.Sprintf("block %d previous block hash mismatch", record.Block.Height)

				return result, nil
			}
		}

		computedBlockHash, err := computeBlockHash(
			record.Block.Height,
			stringValue(record.Block.PreviousBlockHash),
			record.Block.EventHash,
			record.Block.EventType,
			record.Block.ActorUserID,
			hashOccurredAt,
		)
		if err != nil {
			return nil, err
		}

		if computedBlockHash != record.Block.BlockHash {
			result.Valid = false
			result.Failure = fmt.Sprintf("block %d block hash mismatch", record.Block.Height)

			return result, nil
		}

		blockCopy := record.Block
		previousBlock = &blockCopy
	}

	if result.CheckedBlocks == 0 {
		result.Valid = false
		result.Failure = "blockchain is empty"
	}

	return result, nil
}

func marshalCanonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, err
	}

	return compact.Bytes(), nil
}

func canonicalizeJSONBytes(data []byte) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return marshalCanonicalJSON(struct{}{})
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}

	return marshalCanonicalJSON(value)
}

func canonicalizeBlockchainPayloadJSON(data []byte) ([]byte, error) {
	payload, err := parseBlockchainPayloadJSON(data)
	if err != nil {
		return nil, err
	}

	canonicalDataJSON, err := canonicalizeBlockchainEventDataJSON(payload.EventType, payload.Data)
	if err != nil {
		return nil, err
	}
	payload.Data = json.RawMessage(canonicalDataJSON)

	return marshalCanonicalJSON(payload)
}

func legacyCanonicalizeBlockchainPayloadJSON(data []byte) ([]byte, error) {
	payload, err := parseBlockchainPayloadJSON(data)
	if err != nil {
		return nil, err
	}

	canonicalDataJSON, err := canonicalizeJSONBytes(payload.Data)
	if err != nil {
		return nil, err
	}
	payload.Data = json.RawMessage(canonicalDataJSON)

	return marshalCanonicalJSON(payload)
}

func parseBlockchainPayloadJSON(data []byte) (BlockchainEventPayload, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return BlockchainEventPayload{}, errors.New("blockchain payload JSON is empty")
	}

	var payload BlockchainEventPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return BlockchainEventPayload{}, err
	}

	return payload, nil
}

func canonicalizeBlockchainEventDataJSON(eventType string, data []byte) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return marshalCanonicalJSON(struct{}{})
	}

	var value any
	switch eventType {
	case BlockchainEventGenesis:
		value = &struct {
			Message string `json:"message"`
		}{}
	case "assignment_published":
		value = &assignmentPublishedEventData{}
	case "assignment_updated":
		value = &assignmentUpdatedEventData{}
	case "assignment_deleted":
		value = &assignmentDeletedEventData{}
	case "certificate_issued":
		value = &certificateIssuedEventData{}
	case "certificate_revoked":
		value = &certificateRevokedEventData{}
	case "course_created":
		value = &courseCreatedEventData{}
	case "course_updated":
		value = &courseUpdatedEventData{}
	case "course_deleted":
		value = &courseDeletedEventData{}
	case "instructor_assigned":
		value = &courseInstructorAssignedEventData{}
	case "student_enrolled":
		value = &courseStudentEnrolledEventData{}
	case "grade_published", "grade_revised", "grade_updated":
		value = &gradeBlockchainEventData{}
	case "submission_committed":
		value = &submissionCommittedEventData{}
	default:
		return canonicalizeJSONBytes(data)
	}

	if err := json.Unmarshal(data, value); err != nil {
		return nil, err
	}

	return marshalCanonicalJSON(value)
}

func matchesStoredBlockchainPayloadHash(payloadJSON []byte, canonicalPayloadJSON []byte, storedPayloadHash string) (bool, error) {
	if hashBytesSHA256Hex(canonicalPayloadJSON) == storedPayloadHash {
		return true, nil
	}

	legacyPayloadJSON, err := legacyCanonicalizeBlockchainPayloadJSON(payloadJSON)
	if err != nil {
		return false, fmt.Errorf("failed to canonicalize legacy blockchain payload: %w", err)
	}

	return hashBytesSHA256Hex(legacyPayloadJSON) == storedPayloadHash, nil
}

func isExpectedGenesisRecord(record BlockchainRecord, occurredAt time.Time, actualPayloadJSON []byte) bool {
	if record.Block.Height != 0 || record.Block.EventType != BlockchainEventGenesis {
		return false
	}
	if record.Event.EventType != BlockchainEventGenesis || record.Event.EntityType != BlockchainEntitySystem {
		return false
	}
	if record.Event.EntityID == nil || *record.Event.EntityID != BlockchainEntityGenesis {
		return false
	}
	if record.Event.ActorUserID != nil || record.Block.ActorUserID != nil {
		return false
	}

	expectedPayload := BlockchainEventPayload{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: formatBlockchainTime(occurredAt),
		Data:       json.RawMessage(`{"message":"taalam blockchain genesis block"}`),
	}
	expectedPayloadJSON, err := marshalCanonicalJSON(expectedPayload)
	if err != nil {
		return false
	}
	expectedPayloadJSON, err = canonicalizeBlockchainPayloadJSON(expectedPayloadJSON)
	if err != nil {
		return false
	}

	return bytes.Equal(expectedPayloadJSON, actualPayloadJSON)
}

func hashBytesSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func normalizeOptionalUUIDString(raw string) (*string, error) {
	value := trimmedOptionalString(raw)
	if value == nil {
		return nil, nil
	}

	if _, err := parseUUID(*value); err != nil {
		return nil, ErrInvalidBlockchainActorUserID
	}

	return value, nil
}

func parseUUID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrInvalidBlockchainActorUserID
	}

	if _, err := uuid.Parse(value); err != nil {
		return "", ErrInvalidBlockchainActorUserID
	}

	return value, nil
}

func trimmedOptionalString(raw string) *string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}

	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func stringPointer(value string) *string {
	return &value
}

func normalizeBlockchainTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func parseBlockchainTime(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, errors.New("blockchain time is empty")
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}

	return parsed.UTC(), nil
}

func formatBlockchainTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
