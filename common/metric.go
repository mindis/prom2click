package common

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/storage/remote"
)

type Metric struct {
	Metric string
	Hash   string
	TS     *remote.TimeSeries
}

// FingerPrint returns a hashable string from a TimeSeries labels
func FingerPrint(ts *remote.TimeSeries) string {
	nKeys := len(ts.Labels)
	keyVals := make([]string, nKeys, nKeys)
	for i, label := range ts.Labels {
		keyVals[i] = fmt.Sprintf("%s=%s", label.Name, label.Value)
	}
	sort.Strings(keyVals)
	return strings.Join(keyVals, "0xff")
}
