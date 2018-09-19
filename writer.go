package wavefront

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/prompb"
)

type Sender struct {
	Prefix         string
	SourceOverride []string
	pool           *Pool
}

var sanitizedRegex = regexp.MustCompile("[^a-zA-Z\\d_.-]")

var tagValueReplacer = strings.NewReplacer("\"", "\\\"", "*", "-")

var pathReplacer = strings.NewReplacer("_", "_")

type metricPoint struct {
	Metric    string
	Value     float64
	Timestamp int64
	Source    string
	Tags      map[string]string
}

func NewSender(host string) *Sender {
	return &Sender{
		pool: NewPool(host),
	}
}

func (w *Sender) Write(rq prompb.WriteRequest) error {

	// Send Data to Wavefront proxy Server
	connection, err := w.pool.Get()
	if err != nil {
		return fmt.Errorf("Wavefront: TCP connect fail %s", err.Error())
	}
	connection.SetWriteDeadline(time.Now().Add(5 * time.Second))

	for _, ts := range rq.Timeseries {
		for _, metricPoint := range w.buildMetrics(ts) {
			metricLine := w.formatMetricPoint(metricPoint)
			_, err := connection.Write([]byte(metricLine))
			if err != nil {
				return fmt.Errorf("Wavefront: TCP writing error %s", err.Error())
			}
		}
	}

	// Don't be tempted to put this in a "defer". We only want to put things
	// back into the pool if the operation was successful.
	w.pool.Return(connection)
	return nil
}

func (w *Sender) buildMetrics(ts *prompb.TimeSeries) []*metricPoint {
	ret := []*metricPoint{}

	tags := make(map[string]string, len(ts.Labels))
	for _, l := range ts.Labels {
		tags[l.Name] = l.Value
	}
	fieldName := tags["__name__"]
	delete(tags, "__name__")
	for _, value := range ts.Samples {
		name := sanitizedRegex.ReplaceAllLiteralString(w.Prefix+"_"+fieldName, "-")

		metric := &metricPoint{
			Metric:    name,
			Timestamp: value.Timestamp,
		}

		metric.Value = value.Value

		source, tags := w.buildTags(tags)
		metric.Source = source
		metric.Tags = tags

		ret = append(ret, metric)
	}
	return ret
}

func (w *Sender) buildTags(mTags map[string]string) (string, map[string]string) {
	// Remove all empty tags.
	for k, v := range mTags {
		if v == "" {
			delete(mTags, k)
		}
	}
	source := mTags["instance"]
	delete(mTags, "instance")

	return tagValueReplacer.Replace(source), mTags
}

func (w *Sender) formatMetricPoint(metricPoint *metricPoint) string {
	buffer := bytes.NewBufferString("")
	buffer.WriteString(metricPoint.Metric)
	buffer.WriteString(" ")
	buffer.WriteString(strconv.FormatFloat(metricPoint.Value, 'f', 6, 64))
	buffer.WriteString(" ")
	buffer.WriteString(strconv.FormatInt(metricPoint.Timestamp, 10))
	buffer.WriteString(" source=\"")
	buffer.WriteString(metricPoint.Source)
	buffer.WriteString("\"")

	for k, v := range metricPoint.Tags {
		buffer.WriteString(" ")
		buffer.WriteString(sanitizedRegex.ReplaceAllLiteralString(k, "-"))
		buffer.WriteString("=\"")
		buffer.WriteString(tagValueReplacer.Replace(v))
		buffer.WriteString("\"")
	}

	buffer.WriteString("\n")

	return buffer.String()
}
