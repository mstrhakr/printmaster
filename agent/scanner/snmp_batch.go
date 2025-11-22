package scanner

import (
	"context"
	"fmt"

	"github.com/gosnmp/gosnmp"
)

const (
	defaultOIDBatchSize = 3
)

// clusterOIDs splits the provided OID list into fixed-size batches, preserving order.
func clusterOIDs(oids []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = defaultOIDBatchSize
	}
	if len(oids) == 0 {
		return nil
	}

	batches := make([][]string, 0, (len(oids)+batchSize-1)/batchSize)
	for i := 0; i < len(oids); i += batchSize {
		end := i + batchSize
		if end > len(oids) {
			end = len(oids)
		}
		chunk := make([]string, end-i)
		copy(chunk, oids[i:end])
		batches = append(batches, chunk)
	}
	return batches
}

// batchedGet fetches OIDs in clusters, limiting each PDU to batchSize entries.
func batchedGet(ctx context.Context, client SNMPClient, oids []string, batchSize int) ([]gosnmp.SnmpPDU, error) {
	batches := clusterOIDs(oids, batchSize)
	if len(batches) == 0 {
		return nil, nil
	}

	var all []gosnmp.SnmpPDU
	for _, batch := range batches {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		packet, err := client.Get(batch)
		if err != nil {
			return nil, fmt.Errorf("SNMP GET failed for batch (first oid %s): %w", batch[0], err)
		}
		if packet != nil {
			all = append(all, packet.Variables...)
		}
	}
	return all, nil
}
