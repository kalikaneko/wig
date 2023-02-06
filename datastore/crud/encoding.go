package crud

import (
	"encoding/json"
	"io"
)

// Encoding implements the crudlog.Encoding interface, serializing
// Type objects to opaque strings. The current implementation uses
// doubly-encoded JSON (value + type name) for simplicity, though a
// number of alternative schemes can be easily imagined.
type Encoding struct {
	*registry
}

type valueContainer struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

func (e *Encoding) MarshalValue(obj interface{}) ([]byte, error) {
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	m, ok := e.registry.getType(obj)
	if !ok {
		return nil, ErrUnknownType
	}

	return json.Marshal(&valueContainer{
		Type: m.Name(),
		Data: encoded,
	})
}

func (e *Encoding) UnmarshalValue(b []byte) (interface{}, error) {
	var cont valueContainer
	if err := json.Unmarshal(b, &cont); err != nil {
		return nil, err
	}

	m, ok := e.registry.byName[cont.Type]
	if !ok {
		return nil, ErrUnknownType
	}
	value := m.NewInstance()
	return value, json.Unmarshal(cont.Data, value)
}

func (e *Encoding) UnmarshalValueFromReader(input io.Reader) (interface{}, error) {
	var cont valueContainer
	if err := json.NewDecoder(input).Decode(&cont); err != nil {
		return nil, err
	}

	m, ok := e.registry.byName[cont.Type]
	if !ok {
		return nil, ErrUnknownType
	}
	value := m.NewInstance()
	return value, json.Unmarshal(cont.Data, value)
}
