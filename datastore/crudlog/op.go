package crudlog

import (
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
)

type op struct {
	seq       Sequence
	typ       OpType
	timestamp time.Time
	value     interface{}
}

func newOp(typ OpType, value interface{}) *op {
	return &op{
		typ:       typ,
		value:     value,
		timestamp: time.Now(),
	}
}

func (o *op) Seq() Sequence        { return o.seq }
func (o *op) Type() OpType         { return o.typ }
func (o *op) Value() interface{}   { return o.value }
func (o *op) Timestamp() time.Time { return o.timestamp }
func (o *op) WithSequence(seq Sequence) Op {
	newOp := *o
	newOp.seq = seq
	return &newOp
}

func (o *op) serialize(enc Encoding) (*opSerialized, error) {
	b, err := enc.MarshalValue(o.value)
	if err != nil {
		return nil, err
	}
	return &opSerialized{
		Seq:   o.seq,
		Type:  o.typ,
		Value: b,
	}, nil
}

type opSerialized struct {
	Seq       Sequence  `json:"seq" db:"seq"`
	Type      OpType    `json:"type" db:"type"`
	Value     []byte    `json:"value" db:"value"`
	Timestamp time.Time `json:"timestamp" db:"timestamp"`
}

func (o *opSerialized) decode(enc Encoding) (*op, error) {
	v, err := enc.UnmarshalValue(o.Value)
	if err != nil {
		return nil, err
	}
	return &op{
		seq:       o.Seq,
		typ:       o.Type,
		timestamp: o.Timestamp,
		value:     v,
	}, nil
}

func scanOp(rows *sqlx.Rows, encoding Encoding) (*op, error) {
	var ops opSerialized
	if err := rows.StructScan(&ops); err != nil {
		return nil, err
	}
	return ops.decode(encoding)
}

func (op *op) WithEncoding(encoding Encoding) OpWithEncoding {
	return &opWithEncoding{op: op, encoding: encoding}
}

type opWithEncoding struct {
	*op
	encoding Encoding
}

func (op *opWithEncoding) Op() Op { return op.op }

func (op *opWithEncoding) MarshalJSON() ([]byte, error) {
	ops, err := op.serialize(op.encoding)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ops)
}

func (op *opWithEncoding) UnmarshalJSON(b []byte) error {
	var ops opSerialized
	if err := json.Unmarshal(b, &ops); err != nil {
		return err
	}
	newop, err := ops.decode(op.encoding)
	if err != nil {
		return err
	}
	op.op = newop
	return nil
}
