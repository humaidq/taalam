/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestComputeEventHashStable(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	entityID := stringPointer("course-1")
	actorID := stringPointer(uuid.NewString())

	hashA, err := computeEventHash("course_created", "course", entityID, actorID, occurredAt, "payload-hash")
	if err != nil {
		t.Fatalf("computeEventHash() error = %v", err)
	}
	hashB, err := computeEventHash("course_created", "course", entityID, actorID, occurredAt, "payload-hash")
	if err != nil {
		t.Fatalf("computeEventHash() error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected stable event hash, got %q and %q", hashA, hashB)
	}

	hashC, err := computeEventHash("course_created", "course", entityID, actorID, occurredAt, "different-payload-hash")
	if err != nil {
		t.Fatalf("computeEventHash() error = %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected event hash to change when payload hash changes")
	}
}

func TestComputeBlockHashStable(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	actorID := stringPointer(uuid.NewString())

	hashA, err := computeBlockHash(1, "prev-hash", "event-hash", "course_created", actorID, occurredAt)
	if err != nil {
		t.Fatalf("computeBlockHash() error = %v", err)
	}
	hashB, err := computeBlockHash(1, "prev-hash", "event-hash", "course_created", actorID, occurredAt)
	if err != nil {
		t.Fatalf("computeBlockHash() error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected stable block hash, got %q and %q", hashA, hashB)
	}

	hashC, err := computeBlockHash(2, "prev-hash", "event-hash", "course_created", actorID, occurredAt)
	if err != nil {
		t.Fatalf("computeBlockHash() error = %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected block hash to change when block metadata changes")
	}
}

func TestBuildBlockchainPayloadValidation(t *testing.T) {
	if _, err := buildBlockchainPayload(AppendBlockchainEventInput{EntityType: "course"}); err != ErrBlockchainEventTypeRequired {
		t.Fatalf("expected %v, got %v", ErrBlockchainEventTypeRequired, err)
	}

	if _, err := buildBlockchainPayload(AppendBlockchainEventInput{EventType: "course_created"}); err != ErrBlockchainEntityTypeRequired {
		t.Fatalf("expected %v, got %v", ErrBlockchainEntityTypeRequired, err)
	}

	if _, err := buildBlockchainPayload(AppendBlockchainEventInput{
		EventType:   "course_created",
		EntityType:  "course",
		ActorUserID: "not-a-uuid",
		Data:        map[string]string{"code": "CS8602"},
	}); err != ErrInvalidBlockchainActorUserID {
		t.Fatalf("expected %v, got %v", ErrInvalidBlockchainActorUserID, err)
	}
}

func TestBuildBlockchainPayloadCanonicalizesStoredJSON(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 123456789, time.UTC)
	built, err := buildBlockchainPayload(AppendBlockchainEventInput{
		EventType:  "course_created",
		EntityType: "course",
		EntityID:   "course-1",
		OccurredAt: occurredAt,
		Data: map[string]any{
			"title": "Intro to Systems",
			"code":  "CS8602",
		},
	})
	if err != nil {
		t.Fatalf("buildBlockchainPayload() error = %v", err)
	}

	canonicalPayloadJSON, err := canonicalizeBlockchainPayloadJSON(built.PayloadJSON)
	if err != nil {
		t.Fatalf("canonicalizeBlockchainPayloadJSON() error = %v", err)
	}
	if string(built.PayloadJSON) != string(canonicalPayloadJSON) {
		t.Fatalf("expected payload JSON to already be canonical, got %s want %s", built.PayloadJSON, canonicalPayloadJSON)
	}
	if built.PayloadHash != hashBytesSHA256Hex(canonicalPayloadJSON) {
		t.Fatalf("expected payload hash %q to match canonical payload JSON", built.PayloadHash)
	}

	normalizedOccurredAt := normalizeBlockchainTime(occurredAt)
	if !built.OccurredAt.Equal(normalizedOccurredAt) {
		t.Fatalf("expected occurred_at %s, got %s", normalizedOccurredAt, built.OccurredAt)
	}

	payload, err := parseBlockchainPayloadJSON(built.PayloadJSON)
	if err != nil {
		t.Fatalf("parseBlockchainPayloadJSON() error = %v", err)
	}
	if payload.OccurredAt != formatBlockchainTime(normalizedOccurredAt) {
		t.Fatalf("expected payload occurred_at %q, got %q", formatBlockchainTime(normalizedOccurredAt), payload.OccurredAt)
	}
}

func TestCanonicalizeBlockchainPayloadJSONRestoresStructuredDataOrdering(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 123456000, time.UTC)
	built, err := buildBlockchainPayload(AppendBlockchainEventInput{
		EventType:   "course_created",
		EntityType:  "course",
		EntityID:    "course-1",
		ActorUserID: uuid.MustParse("11111111-1111-1111-1111-111111111111").String(),
		OccurredAt:  occurredAt,
		Data: courseCreatedEventData{
			CourseID:  "course-1",
			Code:      "CS8602",
			Title:     "Intro to Systems",
			Term:      "Spring 2026",
			CreatedBy: "11111111-1111-1111-1111-111111111111",
		},
	})
	if err != nil {
		t.Fatalf("buildBlockchainPayload() error = %v", err)
	}

	var payloadMap map[string]any
	if err := json.Unmarshal(built.PayloadJSON, &payloadMap); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	reorderedPayloadJSON, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	canonicalPayloadJSON, err := canonicalizeBlockchainPayloadJSON(reorderedPayloadJSON)
	if err != nil {
		t.Fatalf("canonicalizeBlockchainPayloadJSON() error = %v", err)
	}
	if string(canonicalPayloadJSON) != string(built.PayloadJSON) {
		t.Fatalf("expected canonical payload JSON %s, got %s", built.PayloadJSON, canonicalPayloadJSON)
	}
	if hashBytesSHA256Hex(canonicalPayloadJSON) != built.PayloadHash {
		t.Fatalf("expected payload hash %q to match reordered JSON", built.PayloadHash)
	}
}

func TestVerifyBlockchainRecords(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	genesis, err := buildTestBlockchainRecord(0, nil, AppendBlockchainEventInput{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: occurredAt,
		Data:       map[string]string{"message": "genesis"},
	})
	if err != nil {
		t.Fatalf("buildTestBlockchainRecord() error = %v", err)
	}

	actorID := uuid.NewString()
	record, err := buildTestBlockchainRecord(1, &genesis.Block.BlockHash, AppendBlockchainEventInput{
		EventType:   "course_created",
		EntityType:  "course",
		EntityID:    "course-1",
		ActorUserID: actorID,
		OccurredAt:  occurredAt.Add(time.Minute),
		Data:        map[string]string{"code": "CS8602"},
	})
	if err != nil {
		t.Fatalf("buildTestBlockchainRecord() error = %v", err)
	}

	result, err := verifyBlockchainRecords([]BlockchainRecord{genesis, record})
	if err != nil {
		t.Fatalf("verifyBlockchainRecords() error = %v", err)
	}
	if !result.Valid || result.CheckedBlocks != 2 || result.HeadHeight != 1 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestVerifyBlockchainRecordsDetectsTampering(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	genesis, err := buildTestBlockchainRecord(0, nil, AppendBlockchainEventInput{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: occurredAt,
		Data:       map[string]string{"message": "genesis"},
	})
	if err != nil {
		t.Fatalf("buildTestBlockchainRecord() error = %v", err)
	}

	record, err := buildTestBlockchainRecord(1, &genesis.Block.BlockHash, AppendBlockchainEventInput{
		EventType:  "course_created",
		EntityType: "course",
		EntityID:   "course-1",
		OccurredAt: occurredAt.Add(time.Minute),
		Data:       map[string]string{"code": "CS8602"},
	})
	if err != nil {
		t.Fatalf("buildTestBlockchainRecord() error = %v", err)
	}

	tampered := record
	tampered.Event.PayloadHash = "deadbeef"

	result, err := verifyBlockchainRecords([]BlockchainRecord{genesis, tampered})
	if err != nil {
		t.Fatalf("verifyBlockchainRecords() error = %v", err)
	}
	if result.Valid || result.Failure == "" {
		t.Fatalf("expected verification failure, got %#v", result)
	}
}

func TestVerifyBlockchainRecordsAcceptsLegacyGenesisEventHash(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 5, 17, 39, 0, 753654048, time.UTC)
	genesis, err := buildLegacyTestBlockchainRecord(0, nil, AppendBlockchainEventInput{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: occurredAt,
		Data:       map[string]string{"message": "taalam blockchain genesis block"},
	})
	if err != nil {
		t.Fatalf("buildTestBlockchainRecord() error = %v", err)
	}

	legacyGenesis := genesis
	legacyGenesis.Block.EventHash = "c2b674d8bae35fedf0e46415dca6ed7c9663a6b03208b7a7cfab2d8c1806b3df"
	legacyGenesis.Block.BlockHash = "6a824348b9b276ec757c76793e5af2226eb7bdde517dea6b983da847353e69d5"

	result, err := verifyBlockchainRecords([]BlockchainRecord{legacyGenesis})
	if err != nil {
		t.Fatalf("verifyBlockchainRecords() error = %v", err)
	}
	if !result.Valid || result.CheckedBlocks != 1 || result.HeadHeight != 0 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestVerifyBlockchainRecordsAcceptsLegacyTimestampPrecision(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 10, 12, 0, 0, 123456789, time.UTC)
	genesis, err := buildLegacyTestBlockchainRecord(0, nil, AppendBlockchainEventInput{
		EventType:  BlockchainEventGenesis,
		EntityType: BlockchainEntitySystem,
		EntityID:   BlockchainEntityGenesis,
		OccurredAt: occurredAt,
		Data:       map[string]string{"message": "genesis"},
	})
	if err != nil {
		t.Fatalf("buildLegacyTestBlockchainRecord() error = %v", err)
	}

	actorID := uuid.NewString()
	record, err := buildLegacyTestBlockchainRecord(1, &genesis.Block.BlockHash, AppendBlockchainEventInput{
		EventType:   "course_created",
		EntityType:  "course",
		EntityID:    "course-1",
		ActorUserID: actorID,
		OccurredAt:  occurredAt.Add(time.Minute),
		Data:        map[string]string{"code": "CS8602"},
	})
	if err != nil {
		t.Fatalf("buildLegacyTestBlockchainRecord() error = %v", err)
	}

	result, err := verifyBlockchainRecords([]BlockchainRecord{genesis, record})
	if err != nil {
		t.Fatalf("verifyBlockchainRecords() error = %v", err)
	}
	if !result.Valid || result.CheckedBlocks != 2 || result.HeadHeight != 1 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestVerifyBlockchainRecordsAcceptsLegacyGenericPayloadHash(t *testing.T) {
	occurredAt := time.Date(2026, time.April, 5, 19, 30, 10, 960321000, time.UTC)
	actorID := uuid.MustParse("98118fd5-e025-43cc-b1b5-434944bcef90").String()
	record, err := buildLegacyGenericPayloadHashRecord(0, nil, AppendBlockchainEventInput{
		EventType:   "certificate_issued",
		EntityType:  "certificate",
		EntityID:    "9e3ae412-0c45-468a-b9d4-edec444cb611",
		ActorUserID: actorID,
		OccurredAt:  occurredAt,
		Data: certificateIssuedEventData{
			CertificateID:      "9e3ae412-0c45-468a-b9d4-edec444cb611",
			CertificateCode:    "CERT-FB05D5910BF8",
			CertificateHash:    "b0c83f5fdf2d02fd99cb0c29e323cc74a2a8b0f635b0621739c289f181f3999f",
			CourseID:           "e26d1118-4053-4aad-b5dc-b6469572a9c3",
			StudentID:          "768b7f78-54ff-4ae2-8eb9-06d9a2a03ffb",
			StudentDisplayName: "Student A",
			CourseCode:         "CS8602",
			CourseTitle:        "Something",
			ResultSummary:      "completed",
			GradeSummary:       "",
			IssuedBy:           actorID,
		},
	})
	if err != nil {
		t.Fatalf("buildLegacyGenericPayloadHashRecord() error = %v", err)
	}

	result, err := verifyBlockchainRecords([]BlockchainRecord{record})
	if err != nil {
		t.Fatalf("verifyBlockchainRecords() error = %v", err)
	}
	if !result.Valid || result.CheckedBlocks != 1 || result.HeadHeight != 0 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func buildTestBlockchainRecord(height int64, previousBlockHash *string, input AppendBlockchainEventInput) (BlockchainRecord, error) {
	built, err := buildBlockchainPayload(input)
	if err != nil {
		return BlockchainRecord{}, err
	}

	canonicalPayloadJSON, err := canonicalizeBlockchainPayloadJSON(built.PayloadJSON)
	if err != nil {
		return BlockchainRecord{}, err
	}
	payloadHash := hashBytesSHA256Hex(canonicalPayloadJSON)
	eventHash, err := computeEventHash(built.EventType, built.EntityType, built.EntityID, built.ActorUserID, built.OccurredAt, payloadHash)
	if err != nil {
		return BlockchainRecord{}, err
	}

	blockHash, err := computeBlockHash(height, stringValue(previousBlockHash), eventHash, built.EventType, built.ActorUserID, built.OccurredAt)
	if err != nil {
		return BlockchainRecord{}, err
	}

	blockID := uuid.NewString()
	eventID := uuid.NewString()

	return BlockchainRecord{
		Block: ChainBlock{
			ID:                blockID,
			Height:            height,
			PreviousBlockHash: previousBlockHash,
			BlockHash:         blockHash,
			EventType:         built.EventType,
			EventHash:         eventHash,
			ActorUserID:       built.ActorUserID,
			OccurredAt:        built.OccurredAt,
			CreatedAt:         built.OccurredAt,
		},
		Event: ChainEvent{
			ID:          eventID,
			BlockID:     blockID,
			EventType:   built.EventType,
			EntityType:  built.EntityType,
			EntityID:    built.EntityID,
			PayloadJSON: canonicalPayloadJSON,
			PayloadHash: payloadHash,
			ActorUserID: built.ActorUserID,
			OccurredAt:  built.OccurredAt,
			CreatedAt:   built.OccurredAt,
		},
	}, nil
}

func buildLegacyTestBlockchainRecord(height int64, previousBlockHash *string, input AppendBlockchainEventInput) (BlockchainRecord, error) {
	eventType := input.EventType
	entityType := input.EntityType
	entityID := trimmedOptionalString(input.EntityID)
	actorUserID, err := normalizeOptionalUUIDString(input.ActorUserID)
	if err != nil {
		return BlockchainRecord{}, err
	}

	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	dataJSON, err := marshalCanonicalJSON(input.Data)
	if err != nil {
		return BlockchainRecord{}, err
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
		return BlockchainRecord{}, err
	}
	canonicalPayloadJSON, err := canonicalizeBlockchainPayloadJSON(payloadJSON)
	if err != nil {
		return BlockchainRecord{}, err
	}
	payloadHash := hashBytesSHA256Hex(canonicalPayloadJSON)
	eventHash, err := computeEventHash(eventType, entityType, entityID, actorUserID, occurredAt, payloadHash)
	if err != nil {
		return BlockchainRecord{}, err
	}
	blockHash, err := computeBlockHash(height, stringValue(previousBlockHash), eventHash, eventType, actorUserID, occurredAt)
	if err != nil {
		return BlockchainRecord{}, err
	}

	storedOccurredAt := normalizeBlockchainTime(occurredAt)
	blockID := uuid.NewString()
	eventID := uuid.NewString()

	return BlockchainRecord{
		Block: ChainBlock{
			ID:                blockID,
			Height:            height,
			PreviousBlockHash: previousBlockHash,
			BlockHash:         blockHash,
			EventType:         eventType,
			EventHash:         eventHash,
			ActorUserID:       actorUserID,
			OccurredAt:        storedOccurredAt,
			CreatedAt:         storedOccurredAt,
		},
		Event: ChainEvent{
			ID:          eventID,
			BlockID:     blockID,
			EventType:   eventType,
			EntityType:  entityType,
			EntityID:    entityID,
			PayloadJSON: canonicalPayloadJSON,
			PayloadHash: payloadHash,
			ActorUserID: actorUserID,
			OccurredAt:  storedOccurredAt,
			CreatedAt:   storedOccurredAt,
		},
	}, nil
}

func buildLegacyGenericPayloadHashRecord(height int64, previousBlockHash *string, input AppendBlockchainEventInput) (BlockchainRecord, error) {
	built, err := buildBlockchainPayload(input)
	if err != nil {
		return BlockchainRecord{}, err
	}

	legacyPayloadJSON, err := legacyCanonicalizeBlockchainPayloadJSON(built.PayloadJSON)
	if err != nil {
		return BlockchainRecord{}, err
	}
	payloadHash := hashBytesSHA256Hex(legacyPayloadJSON)
	eventHash, err := computeEventHash(built.EventType, built.EntityType, built.EntityID, built.ActorUserID, built.OccurredAt, payloadHash)
	if err != nil {
		return BlockchainRecord{}, err
	}
	blockHash, err := computeBlockHash(height, stringValue(previousBlockHash), eventHash, built.EventType, built.ActorUserID, built.OccurredAt)
	if err != nil {
		return BlockchainRecord{}, err
	}

	blockID := uuid.NewString()
	eventID := uuid.NewString()

	return BlockchainRecord{
		Block: ChainBlock{
			ID:                blockID,
			Height:            height,
			PreviousBlockHash: previousBlockHash,
			BlockHash:         blockHash,
			EventType:         built.EventType,
			EventHash:         eventHash,
			ActorUserID:       built.ActorUserID,
			OccurredAt:        built.OccurredAt,
			CreatedAt:         built.OccurredAt,
		},
		Event: ChainEvent{
			ID:          eventID,
			BlockID:     blockID,
			EventType:   built.EventType,
			EntityType:  built.EntityType,
			EntityID:    built.EntityID,
			PayloadJSON: built.PayloadJSON,
			PayloadHash: payloadHash,
			ActorUserID: built.ActorUserID,
			OccurredAt:  built.OccurredAt,
			CreatedAt:   built.OccurredAt,
		},
	}, nil
}
