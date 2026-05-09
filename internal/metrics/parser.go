package metrics

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Sample struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

func ParseText(r io.Reader) ([]Sample, error) {
	var samples []Sample
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sample, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

func Find(samples []Sample, name string) []Sample {
	var out []Sample
	for _, sample := range samples {
		if sample.Name == name {
			out = append(out, sample)
		}
	}
	return out
}

func FindPrefix(samples []Sample, prefix string) []Sample {
	var out []Sample
	for _, sample := range samples {
		if sample.Name == prefix || strings.HasPrefix(sample.Name, prefix+"_") {
			out = append(out, sample)
		}
	}
	return out
}

func MaxValue(samples []Sample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	max := samples[0].Value
	for _, sample := range samples[1:] {
		if sample.Value > max {
			max = sample.Value
		}
	}
	return max, true
}

func MinValue(samples []Sample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	min := samples[0].Value
	for _, sample := range samples[1:] {
		if sample.Value < min {
			min = sample.Value
		}
	}
	return min, true
}

func LabelValue(sample Sample, keys ...string) string {
	for _, key := range keys {
		if v, ok := sample.Labels[key]; ok {
			return v
		}
	}
	return ""
}

func parseLine(line string) (Sample, error) {
	nameAndLabels, rest, ok := splitMetricAndValue(line)
	if !ok {
		return Sample{}, fmt.Errorf("missing metric value")
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return Sample{}, fmt.Errorf("missing metric value")
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Sample{}, fmt.Errorf("invalid metric value %q", fields[0])
	}
	name, labels, err := parseNameAndLabels(nameAndLabels)
	if err != nil {
		return Sample{}, err
	}
	return Sample{Name: name, Labels: labels, Value: value}, nil
}

func splitMetricAndValue(line string) (string, string, bool) {
	depth := 0
	inQuote := false
	escaped := false
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inQuote:
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case r == '{' && !inQuote:
			depth++
		case r == '}' && !inQuote && depth > 0:
			depth--
		case (r == ' ' || r == '\t') && depth == 0 && !inQuote:
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
		}
	}
	return "", "", false
}

func parseNameAndLabels(s string) (string, map[string]string, error) {
	open := strings.IndexByte(s, '{')
	if open < 0 {
		if s == "" {
			return "", nil, fmt.Errorf("missing metric name")
		}
		return s, nil, nil
	}
	if !strings.HasSuffix(s, "}") {
		return "", nil, fmt.Errorf("labels are missing closing brace")
	}
	name := s[:open]
	if name == "" {
		return "", nil, fmt.Errorf("missing metric name")
	}
	labels, err := parseLabels(s[open+1 : len(s)-1])
	return name, labels, err
}

func parseLabels(s string) (map[string]string, error) {
	labels := map[string]string{}
	for strings.TrimSpace(s) != "" {
		s = strings.TrimSpace(s)
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid label set")
		}
		key := strings.TrimSpace(s[:eq])
		rest := strings.TrimSpace(s[eq+1:])
		if !strings.HasPrefix(rest, "\"") {
			return nil, fmt.Errorf("label %q value must be quoted", key)
		}
		value, consumed, err := parseQuoted(rest)
		if err != nil {
			return nil, fmt.Errorf("label %q: %w", key, err)
		}
		labels[key] = value
		rest = strings.TrimSpace(rest[consumed:])
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, ",") {
			return nil, fmt.Errorf("labels must be comma separated")
		}
		s = rest[1:]
	}
	return labels, nil
}

func parseQuoted(s string) (string, int, error) {
	var b strings.Builder
	escaped := false
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if escaped {
			switch ch {
			case 'n':
				b.WriteByte('\n')
			case '\\', '"':
				b.WriteByte(ch)
			default:
				b.WriteByte(ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return b.String(), i + 1, nil
		}
		b.WriteByte(ch)
	}
	return "", 0, fmt.Errorf("unterminated quoted string")
}
