package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/ndms"
	"github.com/hoaxisr/awg-manager/internal/sys/ndmsinfo"
	"github.com/hoaxisr/awg-manager/internal/sys/osdetect"
)

func extractASCSignatures(raw json.RawMessage) (i1, i2, i3, i4, i5 string, _ error) {
	var ext ndms.ASCParamsExtended
	if err := json.Unmarshal(raw, &ext); err != nil {
		return "", "", "", "", "", err
	}
	return ext.I1, ext.I2, ext.I3, ext.I4, ext.I5, nil
}

func stripASCSignatures(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse ASC params: %w", err)
	}
	delete(obj, "i1")
	delete(obj, "i2")
	delete(obj, "i3")
	delete(obj, "i4")
	delete(obj, "i5")
	return marshalNoEscape(obj)
}

func isASCRequiredValueEmpty(v json.RawMessage) bool {
	trimmed := bytes.TrimSpace(v)
	if len(trimmed) == 0 {
		return true
	}
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`)) {
		return true
	}
	return false
}

func getASCPositiveNumber(obj map[string]json.RawMessage, key string) (float64, error) {
	v, ok := obj[key]
	if !ok || isASCRequiredValueEmpty(v) {
		return 0, fmt.Errorf("ASC parameter %s is required", key)
	}

	var n float64
	if err := json.Unmarshal(v, &n); err != nil {
		return 0, fmt.Errorf("ASC parameter %s must be a number", key)
	}
	if n <= 0 {
		return 0, fmt.Errorf("ASC parameter %s must be greater than zero", key)
	}
	return n, nil
}

func isASCDisabledState(obj map[string]json.RawMessage) bool {
	requiredZero := []string{"jc", "jmin", "jmax", "s1", "s2"}
	for _, key := range requiredZero {
		v, ok := obj[key]
		if !ok {
			return false
		}
		var n float64
		if err := json.Unmarshal(v, &n); err != nil || n != 0 {
			return false
		}
	}

	for _, key := range []string{"h1", "h2", "h3", "h4"} {
		v, ok := obj[key]
		if !ok {
			return false
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil || strings.TrimSpace(s) != "" {
			return false
		}
	}

	for _, key := range []string{"s3", "s4"} {
		v, ok := obj[key]
		if !ok {
			continue
		}
		if isASCRequiredValueEmpty(v) {
			continue
		}
		var n float64
		if err := json.Unmarshal(v, &n); err != nil || n != 0 {
			return false
		}
	}

	return true
}

func validateASCParamsRequired(raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("parse ASC params: %w", err)
	}
	if isASCDisabledState(obj) {
		return nil
	}

	if _, err := getASCPositiveNumber(obj, "jc"); err != nil {
		return err
	}
	jmin, err := getASCPositiveNumber(obj, "jmin")
	if err != nil {
		return err
	}
	jmax, err := getASCPositiveNumber(obj, "jmax")
	if err != nil {
		return err
	}
	if jmax <= jmin {
		return fmt.Errorf("ASC parameter jmax must be greater than jmin")
	}
	if _, err := getASCPositiveNumber(obj, "s1"); err != nil {
		return err
	}
	if _, err := getASCPositiveNumber(obj, "s2"); err != nil {
		return err
	}

	requiredText := []string{"h1", "h2", "h3", "h4"}
	for _, key := range requiredText {
		v, ok := obj[key]
		if !ok || isASCRequiredValueEmpty(v) {
			return fmt.Errorf("ASC parameter %s is required", key)
		}
	}

	_, hasS3 := obj["s3"]
	_, hasS4 := obj["s4"]
	if hasS3 || hasS4 {
		if _, err := getASCPositiveNumber(obj, "s3"); err != nil {
			return err
		}
		if _, err := getASCPositiveNumber(obj, "s4"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) generateDefaultASCParams() (json.RawMessage, error) {
	extended := osdetect.AtLeast(5, 1)
	hRanges := ndmsinfo.SupportsHRanges()
	return generateASCParamsRaw(extended, hRanges, rand.NewSource(time.Now().UnixNano()))
}

func (s *Service) applyASCParams(ctx context.Context, ifaceName string, raw json.RawMessage) error {
	if err := validateASCParamsRequired(raw); err != nil {
		return err
	}
	stripped, err := stripASCSignatures(raw)
	if err != nil {
		return err
	}
	if err := s.rciSetASCParams(ctx, ifaceName, stripped); err != nil {
		return fmt.Errorf("set ASC params: %w", err)
	}
	return nil
}
