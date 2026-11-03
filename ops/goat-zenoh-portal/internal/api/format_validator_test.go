package api_test

import (
	"net/http"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildFDSWithMessages returns a serialized FileDescriptorSet whose
// single file has the named top-level messages. Used by the
// format-validator tests below.
func buildFDSWithMessages(t *testing.T, messages ...string) []byte {
	t.Helper()
	syntax := "proto3"
	pkg := "test"
	fname := "test.proto"
	msgs := make([]*descriptorpb.DescriptorProto, len(messages))
	for i, n := range messages {
		nm := n
		msgs[i] = &descriptorpb.DescriptorProto{Name: &nm}
	}
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name: &fname, Package: &pkg, Syntax: &syntax, MessageType: msgs,
		}},
	}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	return b
}

// TestSchema_UploadRejectsFormatFDSMismatch closes the DRS-RCA loop at
// the write boundary: topic declared `protobuf:Track`, vendor uploads
// a .fds containing only Status + SoftwareVersion → portal returns 422
// with a structured `format_mismatch` error code carrying the suggested
// correction.
func TestSchema_UploadRejectsFormatFDSMismatch(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	// Build an .fds with top-level Status + SoftwareVersion — no Track.
	body, ctype := uploadSchemaForm(t,
		buildFDSWithMessages(t, "Status", "SoftwareVersion"),
		"v1", "none", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 format_mismatch, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	body2 := mustReadBody(t, resp)
	if !strings.Contains(body2, "format_mismatch") {
		t.Errorf("expected error code 'format_mismatch' in body, got: %s", body2)
	}
	if !strings.Contains(body2, "protobuf:Track") {
		t.Errorf("body should name the declared format 'protobuf:Track': %s", body2)
	}
	if !strings.Contains(body2, "Status") {
		t.Errorf("body should preview the messages actually present in the .fds: %s", body2)
	}
	if !strings.Contains(body2, "most likely correct") {
		t.Errorf("body should suggest a correction: %s", body2)
	}
}

// TestUpdateTopic_RejectsFormatChangeBreakingExistingFDS exercises the
// PATCH path: topic + .fds already in steady state, operator tries to
// move format to a name not in the uploaded .fds → 422 format_mismatch.
func TestUpdateTopic_RejectsFormatChangeBreakingExistingFDS(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	// Land a valid Track .fds first.
	body, ctype := uploadSchemaForm(t, validTrackFDSBytes(t, ""), "v1", "none", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed upload: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	// Try to PATCH format to something not in the existing .fds.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID), apiKey, map[string]any{
		"format": "protobuf:Helicopter",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 format_mismatch on PATCH, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	respBody := mustReadBody(t, resp)
	if !strings.Contains(respBody, "format_mismatch") {
		t.Errorf("expected error code 'format_mismatch' on PATCH: %s", respBody)
	}
	if !strings.Contains(respBody, "Helicopter") {
		t.Errorf("PATCH error should name the new format: %s", respBody)
	}
}

// TestUpdateTopic_AcceptsFormatChangeMatchingExistingFDS verifies the
// happy PATCH path: format change to a name that IS in the .fds works.
// Use a multi-message .fds so we have a real alternative.
func TestUpdateTopic_AcceptsFormatChangeMatchingExistingFDS(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	// Register topic with format matching one of the two messages.
	apiKey, topicID, _ := registerVendorAndTopicWithFormat(t, ts.URL, c, "protobuf:Alpha")

	body, ctype := uploadSchemaForm(t, buildFDSWithMessages(t, "Alpha", "Beta"), "v1", "none", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed upload: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	// PATCH format to Beta — also a top-level message in the same .fds.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID), apiKey, map[string]any{
		"format": "protobuf:Beta",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on PATCH to matching format, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}

// TestUpdateTopic_AcceptsFormatChangeWhenNoFDSYet — when no schema is
// uploaded the validator is deferred to upload-time, so PATCH'ing format
// freely is allowed.
func TestUpdateTopic_AcceptsFormatChangeWhenNoFDSYet(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	resp := doJSON(t, c, "PATCH", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID), apiKey, map[string]any{
		"format": "protobuf:WhateverShape",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (no .fds → no validator), got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}
