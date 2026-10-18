package domain

import "encoding/json"

// MaxMetadataBytes is the largest accepted metadata object, measured as JSON.
const MaxMetadataBytes = 8 << 10

// Metadata holds free-form client attributes. It is always a JSON object.
type Metadata map[string]any

func validateMetadata(v *fieldErrors, field string, m Metadata) {
	raw, err := json.Marshal(m)
	if err != nil {
		v.add(field, "não é um objeto JSON válido")
		return
	}
	if len(raw) > MaxMetadataBytes {
		v.add(field, "excede %d bytes (recebido %d)", MaxMetadataBytes, len(raw))
	}
}

func (m Metadata) orEmpty() Metadata {
	if m == nil {
		return Metadata{}
	}
	return m
}
