package common

import "fmt"

type MetricBatch struct {
	size    int
	index   int
	buff    []*Metric
	metrics chan []*Metric
}

func NewMetricBatch(size int, c chan []*Metric) *MetricBatch {
	b := new(MetricBatch)
	b.size = size
	b.index = 0
	b.buff = make([]*Metric, size, size)
	b.metrics = c
	return b
}

func (b *MetricBatch) Add(m *Metric) {
	if b.index < b.size {
		// append metric to buffer
		b.buff[b.index] = m
		b.index++
	} else {
		// buffer full, copy and push to channel
		c := make([]*Metric, b.index, b.index)
		copied := copy(c, b.buff)
		if copied != b.index {
			err := "Error: Batch: failed to copy metrics: %d != %d\n"
			fmt.Printf(fmt.Sprintf(err, b.index, copied))
		} else {
			// push to channel
			b.metrics <- c
		}
		// reset index and buffer
		b.index = 0
		for i := 0; i < b.size; i++ {
			b.buff[i] = nil
		}
	}
}

func (b *MetricBatch) Flush() {
	// push buffer to channel if there are any entries
	// use this on eg. shutdown to force process
	// partial batches
	if b.index > 0 {
		c := make([]*Metric, b.index, b.index)
		copied := copy(c, b.buff)
		if copied != b.index {
			err := "Error: Batch: failed to copy metrics: %d != %d\n"
			fmt.Printf(fmt.Sprintf(err, b.index, copied))
		} else {
			// push to channel
			b.metrics <- c
		}
		// reset index and buffer
		b.index = 0
		for i := 0; i < b.size; i++ {
			b.buff[i] = nil
		}
	}
}
