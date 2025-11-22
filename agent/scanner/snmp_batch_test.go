package scanner

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestClusterOIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		oids      []string
		batchSize int
		want      [][]string
	}{
		{
			name:      "exact",
			oids:      []string{"1", "2", "3"},
			batchSize: 2,
			want: [][]string{
				{"1", "2"},
				{"3"},
			},
		},
		{
			name:      "default size",
			oids:      []string{"1", "2", "3", "4"},
			batchSize: 0,
			want: [][]string{
				{"1", "2", "3"},
				{"4"},
			},
		},
		{
			name:      "empty",
			oids:      nil,
			batchSize: 2,
			want:      nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clusterOIDs(tc.oids, tc.batchSize)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("clusterOIDs(%v, %d) = %v", tc.oids, tc.batchSize, got)
			}
		})
	}
}

func TestBatchedGetSplitsRequests(t *testing.T) {
	t.Parallel()

	mock := &mockSNMPClient{
		getResults: []*gosnmp.SnmpPacket{
			{Variables: []gosnmp.SnmpPDU{{Name: "1", Value: 1}}},
			{Variables: []gosnmp.SnmpPDU{{Name: "4", Value: 4}}},
		},
	}

	oids := []string{"1", "2", "3", "4", "5"}
	ctx := context.Background()

	pdus, err := batchedGet(ctx, mock, oids, 3)
	if err != nil {
		t.Fatalf("batchedGet returned error: %v", err)
	}
	if mock.getCalls != 2 {
		t.Fatalf("expected 2 GET calls, got %d", mock.getCalls)
	}
	if len(mock.getInputs) != 2 || len(mock.getInputs[0]) != 3 || len(mock.getInputs[1]) != 2 {
		t.Fatalf("unexpected batching: %v", mock.getInputs)
	}
	if len(pdus) != 2 {
		t.Fatalf("expected 2 PDUs, got %d", len(pdus))
	}
	if pdus[0].Name != "1" || pdus[1].Name != "4" {
		t.Fatalf("unexpected PDU order: %+v", pdus)
	}
}

func TestBatchedGetHonorsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockSNMPClient{}

	if _, err := batchedGet(ctx, mock, []string{"1"}, 2); err == nil {
		t.Fatalf("expected context error")
	}
}
