/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

type blockchainRecordItem struct {
	BlockHeight int64
	EventType   string
	EntityType  string
	EntityID    string
	ActorLabel  string
	OccurredAt  string
	BlockHash   string
	EventHash   string
	PayloadJSON string
}

type blockchainVerificationItem struct {
	Valid         bool
	CheckedBlocks int
	HeadHeight    int64
	HeadHash      string
	Failure       string
}

// ChainExplorer renders the blockchain explorer and verification page.
func ChainExplorer(c flamego.Context, t template.Template, data template.Data) {
	setPage(data, "Chain Explorer")
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Chain Explorer", URL: "/chain", IsCurrent: true},
	})
	data["IsChain"] = true

	records, err := db.ListBlockchainRecords(c.Request().Context(), 100)
	if err != nil {
		logger.Error("failed to list blockchain records", "error", err)
		data["Error"] = "Failed to load blockchain records"
		t.HTML(http.StatusInternalServerError, "chain")

		return
	}

	items := make([]blockchainRecordItem, 0, len(records))
	for _, record := range records {
		items = append(items, makeBlockchainRecordItem(record))
	}
	data["Records"] = items

	head, err := db.GetBlockchainHead(c.Request().Context())
	if err == nil && head != nil {
		data["HeadHeight"] = head.Height
		data["HeadHash"] = head.BlockHash
	}

	if strings.EqualFold(strings.TrimSpace(c.Query("verify")), "1") {
		verification, err := db.VerifyBlockchain(c.Request().Context())
		if err != nil {
			logger.Error("failed to verify blockchain", "error", err)
			data["Error"] = "Failed to verify blockchain"
		} else {
			data["Verification"] = blockchainVerificationItem{
				Valid:         verification.Valid,
				CheckedBlocks: verification.CheckedBlocks,
				HeadHeight:    verification.HeadHeight,
				HeadHash:      verification.HeadHash,
				Failure:       verification.Failure,
			}
		}
	}

	t.HTML(http.StatusOK, "chain")
}

// EntityAudit renders blockchain events linked to one entity.
func EntityAudit(c flamego.Context, t template.Template, data template.Data) {
	setPage(data, "Entity Audit")
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Chain Explorer", URL: "/chain"},
		{Name: "Entity Audit", IsCurrent: true},
	})
	data["IsChain"] = true

	entityType := strings.TrimSpace(c.Param("entityType"))
	entityID := strings.TrimSpace(c.Param("entityID"))
	if entityType == "" || entityID == "" {
		c.Redirect("/chain", http.StatusSeeOther)

		return
	}

	records, err := db.ListBlockchainRecordsForEntity(c.Request().Context(), entityType, entityID)
	if err != nil {
		logger.Error("failed to list entity audit records", "error", err)
		data["Error"] = "Failed to load audit records"
		t.HTML(http.StatusInternalServerError, "entity_audit")

		return
	}

	items := make([]blockchainRecordItem, 0, len(records))
	for _, record := range records {
		items = append(items, makeBlockchainRecordItem(record))
	}

	data["EntityType"] = entityType
	data["EntityID"] = entityID
	data["Records"] = items

	t.HTML(http.StatusOK, "entity_audit")
}

func makeBlockchainRecordItem(record db.BlockchainRecordSummary) blockchainRecordItem {
	payloadJSON := "{}"
	if len(record.PayloadJSON) > 0 {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, record.PayloadJSON, "", "  "); err == nil {
			payloadJSON = pretty.String()
		} else {
			payloadJSON = string(record.PayloadJSON)
		}
	}

	return blockchainRecordItem{
		BlockHeight: record.BlockHeight,
		EventType:   record.EventType,
		EntityType:  record.EntityType,
		EntityID:    stringValue(record.EntityID),
		ActorLabel:  actorLabel(record.ActorDisplayName, record.ActorUsername, record.ActorUserID),
		OccurredAt:  record.OccurredAt.Format("Jan 2, 2006 15:04:05 MST"),
		BlockHash:   record.BlockHash,
		EventHash:   record.EventHash,
		PayloadJSON: payloadJSON,
	}
}

func actorLabel(displayName *string, username *string, userID *string) string {
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		if username != nil && strings.TrimSpace(*username) != "" {
			return strings.TrimSpace(*displayName) + " (" + strings.TrimSpace(*username) + ")"
		}

		return strings.TrimSpace(*displayName)
	}
	if username != nil && strings.TrimSpace(*username) != "" {
		return strings.TrimSpace(*username)
	}
	if userID != nil && strings.TrimSpace(*userID) != "" {
		return strings.TrimSpace(*userID)
	}

	return "system"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
