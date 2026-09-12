package telemetry

import "encoding/json"

// Provenance identifies where a metric came from. It is intentionally a small
// closed vocabulary so callers cannot make an unsupported figure look measured.
type Provenance string

const (
	ProvenanceObserved                Provenance = "observed"
	ProvenanceEstimated               Provenance = "estimated"
	ProvenanceBenchmarkCounterfactual Provenance = "benchmark_counterfactual"
	ProvenanceUnsupported             Provenance = "unsupported"
)

// MetricProvenance labels every numeric metric independently. A request can,
// for example, have provider-observed input tokens and DWYT-estimated context
// sizes without collapsing those two claims into one broad observed flag.
type MetricProvenance map[string]Provenance

const (
	MetricInputTokens               = "input_tokens"
	MetricUncachedInputTokens       = "uncached_input_tokens"
	MetricCachedInputTokens         = "cached_input_tokens"
	MetricCacheWriteTokens          = "cache_write_tokens"
	MetricOutputTokens              = "output_tokens"
	MetricReasoningTokens           = "reasoning_tokens"
	MetricToolTokens                = "tool_tokens"
	MetricContextBeforeDWYT         = "context_before_dwyt"
	MetricContextAfterDWYT          = "context_after_dwyt"
	MetricCompressionMetadataTokens = "compression_metadata_tokens"
	MetricEstimatedCostUSD          = "estimated_cost_usd"
	MetricActualCostUSD             = "actual_cost_usd"
	MetricLatencyMS                 = "latency_ms"
	MetricRequests                  = "requests"
	MetricObservedRequests          = "observed_requests"
	MetricCacheHitPct               = "cache_hit_pct"
	MetricContextBefore             = "context_before"
	MetricContextAfter              = "context_after"
	MetricContextReductionPct       = "context_reduction_pct"
	MetricAvoidedTokens             = "avoided_tokens"
	MetricObservedCostUSD           = "observed_cost_usd"
	MetricTasks                     = "tasks"
	MetricTasksSucceeded            = "tasks_succeeded"
	MetricCompletionPct             = "completion_pct"
	MetricCostPerCompletedTask      = "cost_per_completed_task"
	MetricTokensPerCompletedTask    = "tokens_per_completed_task"
	MetricAvgAttempts               = "avg_attempts"
)

var requestMetricNames = []string{
	MetricInputTokens,
	MetricUncachedInputTokens,
	MetricCachedInputTokens,
	MetricCacheWriteTokens,
	MetricOutputTokens,
	MetricReasoningTokens,
	MetricToolTokens,
	MetricContextBeforeDWYT,
	MetricContextAfterDWYT,
	MetricCompressionMetadataTokens,
	MetricEstimatedCostUSD,
	MetricActualCostUSD,
	MetricLatencyMS,
}

// RequestMetricNames returns a copy of the request-side metric vocabulary so a
// caller can explicitly label every reported or unsupported value.
func RequestMetricNames() []string {
	return append([]string(nil), requestMetricNames...)
}

// For returns unsupported for a missing or invalid entry. Missing provenance is
// never silently promoted to a measured-looking value.
func (p MetricProvenance) For(metric string) Provenance {
	if p == nil {
		return ProvenanceUnsupported
	}
	if value, ok := p[metric]; ok && validProvenance(value) {
		return value
	}
	return ProvenanceUnsupported
}

func validProvenance(value Provenance) bool {
	switch value {
	case ProvenanceObserved, ProvenanceEstimated, ProvenanceBenchmarkCounterfactual, ProvenanceUnsupported:
		return true
	default:
		return false
	}
}

func requestMetricPresence(e RequestEvent) map[string]bool {
	return map[string]bool{
		MetricInputTokens:               e.InputTokens != nil,
		MetricUncachedInputTokens:       e.UncachedInputTokens != nil,
		MetricCachedInputTokens:         e.CachedInputTokens != nil,
		MetricCacheWriteTokens:          e.CacheWriteTokens != nil,
		MetricOutputTokens:              e.OutputTokens != nil,
		MetricReasoningTokens:           e.ReasoningTokens != nil,
		MetricToolTokens:                e.ToolTokens != nil,
		MetricContextBeforeDWYT:         e.ContextBefore != nil,
		MetricContextAfterDWYT:          e.ContextAfter != nil,
		MetricCompressionMetadataTokens: e.CompressionMetadataTokens != nil,
		MetricEstimatedCostUSD:          e.EstimatedCostUSD != nil,
		MetricActualCostUSD:             e.ActualCostUSD != nil,
		MetricLatencyMS:                 e.LatencyMS != nil,
	}
}

func defaultRequestProvenance(metric string, observed bool) Provenance {
	switch metric {
	case MetricActualCostUSD:
		return ProvenanceObserved
	case MetricEstimatedCostUSD, MetricContextBeforeDWYT, MetricContextAfterDWYT, MetricCompressionMetadataTokens:
		return ProvenanceEstimated
	case MetricInputTokens, MetricUncachedInputTokens, MetricCachedInputTokens,
		MetricCacheWriteTokens, MetricOutputTokens, MetricReasoningTokens,
		MetricToolTokens, MetricLatencyMS:
		if observed {
			return ProvenanceObserved
		}
		return ProvenanceEstimated
	default:
		return ProvenanceUnsupported
	}
}

func normalizeRequestProvenance(input MetricProvenance, observed bool, present map[string]bool) MetricProvenance {
	out := make(MetricProvenance, len(requestMetricNames))
	for _, metric := range requestMetricNames {
		if !present[metric] {
			out[metric] = ProvenanceUnsupported
			continue
		}
		if value, ok := input[metric]; ok {
			if validProvenance(value) {
				out[metric] = value
			} else {
				out[metric] = ProvenanceUnsupported
			}
			continue
		}
		out[metric] = defaultRequestProvenance(metric, observed)
	}
	return out
}

func decodeRequestProvenance(raw string, observed bool, present map[string]bool) MetricProvenance {
	var decoded MetricProvenance
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return unsupportedRequestProvenance()
		}
	}
	return normalizeRequestProvenance(decoded, observed, present)
}

func unsupportedRequestProvenance() MetricProvenance {
	out := make(MetricProvenance, len(requestMetricNames))
	for _, metric := range requestMetricNames {
		out[metric] = ProvenanceUnsupported
	}
	return out
}

type metricRollup struct {
	reported                int
	observed                bool
	estimated               bool
	benchmarkCounterfactual bool
	unsupported             bool
}

type metricRollups map[string]*metricRollup

func newMetricRollups() metricRollups {
	out := make(metricRollups, len(requestMetricNames))
	for _, metric := range requestMetricNames {
		out[metric] = &metricRollup{}
	}
	return out
}

func (r metricRollups) add(metric string, reported bool, provenance Provenance) {
	rollup, ok := r[metric]
	if !ok {
		rollup = &metricRollup{}
		r[metric] = rollup
	}
	if !reported {
		return
	}
	rollup.reported++
	switch provenance {
	case ProvenanceObserved:
		rollup.observed = true
	case ProvenanceEstimated:
		rollup.estimated = true
	case ProvenanceBenchmarkCounterfactual:
		rollup.benchmarkCounterfactual = true
	default:
		rollup.unsupported = true
	}
}

func (r metricRollups) value(metric string, requests int) Provenance {
	rollup := r[metric]
	if requests == 0 || rollup == nil || rollup.reported != requests || rollup.unsupported {
		return ProvenanceUnsupported
	}
	if rollup.benchmarkCounterfactual {
		return ProvenanceBenchmarkCounterfactual
	}
	if rollup.estimated {
		return ProvenanceEstimated
	}
	if rollup.observed {
		return ProvenanceObserved
	}
	return ProvenanceUnsupported
}

func (r metricRollups) allReported(requests int, metrics ...string) bool {
	if requests == 0 {
		return false
	}
	for _, metric := range metrics {
		if r.value(metric, requests) == ProvenanceUnsupported {
			return false
		}
	}
	return true
}

func aggregateProvenance(values ...Provenance) Provenance {
	if len(values) == 0 {
		return ProvenanceUnsupported
	}
	seenObserved := false
	seenEstimated := false
	seenCounterfactual := false
	for _, value := range values {
		switch value {
		case ProvenanceUnsupported:
			return ProvenanceUnsupported
		case ProvenanceEstimated:
			seenEstimated = true
		case ProvenanceBenchmarkCounterfactual:
			seenCounterfactual = true
		case ProvenanceObserved:
			seenObserved = true
		default:
			return ProvenanceUnsupported
		}
	}
	if seenCounterfactual {
		return ProvenanceBenchmarkCounterfactual
	}
	if seenEstimated {
		return ProvenanceEstimated
	}
	if seenObserved {
		return ProvenanceObserved
	}
	return ProvenanceUnsupported
}
