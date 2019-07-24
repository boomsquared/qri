package lib

import (
	"fmt"
	"encoding/json"

	"github.com/ghodss/yaml"
	"github.com/qri-io/dataset"
)

// Encode serializes data to a given format
func Encode(format string, cfg dataset.FormatConfig, val interface{}) ([]byte, error) {
	switch format {
	case "json":
		// Pretty defaults to true for the dataset head, unless explicitly set in the config.
		pretty := true
		if cfg != nil {
			pvalue, ok := cfg.Map()["pretty"].(bool)
			if ok {
				pretty = pvalue
			}
		}
		if pretty {
			return json.MarshalIndent(val, "", " ")
		} else {
			return json.Marshal(val)
		}
	case "yaml", "":
		return yaml.Marshal(val)
	default:
		return nil, fmt.Errorf("unknown format: \"%s\"", format)
	}
}