package controllers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// evaluateCondition evaluates a step condition expression against step outputs.
// Supports patterns like:
//   - steps.detect.output.anomalyDetected == true
//   - steps.analyze.output.issues > 0
//   - steps.scan.output.severity == "critical"
//   - steps.check.output.healthy != false
//
// Returns true if the condition is met (step should execute), false to skip.
func evaluateCondition(expr string, outputs map[string]string) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "true" {
		return true, nil
	}
	if expr == "false" {
		return false, nil
	}

	// Match: steps.<step>.output.<key> <op> <value>
	// Also: steps."<step>".output.<key> <op> <value>
	pattern := regexp.MustCompile(`steps\.(?:"([^"]+)"|(\w[\w-]*))\.\s*output\.(\w[\w.]*)\s*(==|!=|>|>=|<|<=|in)\s*(.+)`)
	m := pattern.FindStringSubmatch(expr)
	if m == nil {
		// Try simple boolean reference: steps.<step>.output.<key>
		boolPattern := regexp.MustCompile(`^steps\.(?:"([^"]+)"|(\w[\w-]*))\.\s*output\.(\w[\w.]*)$`)
		bm := boolPattern.FindStringSubmatch(expr)
		if bm != nil {
			stepName := bm[1]
			if stepName == "" {
				stepName = bm[2]
			}
			key := bm[3]
			val, err := resolveOutputValue(outputs, stepName, key)
			if err != nil {
				return false, nil // missing output = condition not met
			}
			return isTruthy(val), nil
		}
		return false, fmt.Errorf("unsupported condition expression: %s", expr)
	}

	stepName := m[1]
	if stepName == "" {
		stepName = m[2]
	}
	key := m[3]
	op := m[4]
	expected := strings.TrimSpace(m[5])

	actual, err := resolveOutputValue(outputs, stepName, key)
	if err != nil {
		return false, nil // missing output = condition not met
	}

	return compareValues(actual, op, expected)
}

// resolveOutputValue extracts a value from step outputs using dot-notation keys.
func resolveOutputValue(outputs map[string]string, stepName, key string) (interface{}, error) {
	outputJSON, ok := outputs[stepName]
	if !ok {
		return nil, fmt.Errorf("step %q has no output", stepName)
	}

	var outputMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(outputJSON), &outputMap); err != nil {
		return nil, err
	}

	// Navigate dot-notation keys (e.g., "metrics.count")
	keys := strings.Split(key, ".")
	current := json.RawMessage(outputJSON)
	for _, k := range keys {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(current, &m); err != nil {
			return nil, fmt.Errorf("key %q not navigable in output", k)
		}
		val, ok := m[k]
		if !ok {
			return nil, fmt.Errorf("key %q not found", k)
		}
		current = val
	}

	// Parse the final value
	var result interface{}
	if err := json.Unmarshal(current, &result); err != nil {
		return string(current), nil
	}
	return result, nil
}

// compareValues compares an actual value against an expected value using an operator.
func compareValues(actual interface{}, op, expected string) (bool, error) {
	// Remove quotes from expected string values
	expected = strings.Trim(expected, `"'`)

	switch op {
	case "==":
		return fmt.Sprintf("%v", actual) == expected, nil
	case "!=":
		return fmt.Sprintf("%v", actual) != expected, nil
	case ">", ">=", "<", "<=":
		return compareNumeric(actual, op, expected)
	case "in":
		// Check if value is in a list: ["a", "b", "c"]
		return containsValue(actual, expected), nil
	}
	return false, fmt.Errorf("unsupported operator: %s", op)
}

func compareNumeric(actual interface{}, op, expected string) (bool, error) {
	var actualNum, expectedNum float64

	switch v := actual.(type) {
	case float64:
		actualNum = v
	case int64:
		actualNum = float64(v)
	case json.Number:
		var err error
		actualNum, err = v.Float64()
		if err != nil {
			return false, err
		}
	default:
		var err error
		actualNum, err = strconv.ParseFloat(fmt.Sprintf("%v", actual), 64)
		if err != nil {
			return false, fmt.Errorf("cannot compare non-numeric value %v", actual)
		}
	}

	var err error
	expectedNum, err = strconv.ParseFloat(expected, 64)
	if err != nil {
		return false, fmt.Errorf("expected value %q is not numeric", expected)
	}

	switch op {
	case ">":
		return actualNum > expectedNum, nil
	case ">=":
		return actualNum >= expectedNum, nil
	case "<":
		return actualNum < expectedNum, nil
	case "<=":
		return actualNum <= expectedNum, nil
	}
	return false, nil
}

func isTruthy(val interface{}) bool {
	switch v := val.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != "" && v != "false" && v != "null"
	case nil:
		return false
	default:
		return true
	}
}

func containsValue(actual interface{}, listStr string) bool {
	actualStr := fmt.Sprintf("%v", actual)
	// Parse list like ["critical", "high"]
	listStr = strings.Trim(listStr, "[]")
	for _, item := range strings.Split(listStr, ",") {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"'`)
		if item == actualStr {
			return true
		}
	}
	return false
}
