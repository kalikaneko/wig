package crudlog

// Maintain the SQL tables necessary to the log's operation.

type sqlLogger struct {
	encoding Encoding
}

func (l *sqlLogger) AppendToLog(tx Transaction, opIntf Op) error {
	ops, err := opIntf.(*op).serialize(l.encoding)
	if err != nil {
		return err
	}
	_, err = tx.Tx().NamedExec(`
		INSERT INTO log 
                  (seq, type, timestamp, value)
                VALUES
                  (:seq, :type, :timestamp, :value)
`, ops)
	return err
}

func (l *sqlLogger) QueryLogSince(tx Transaction, seq Sequence) ([]Op, error) {
	rows, err := tx.Tx().Queryx(`
		SELECT
		   seq, type, timestamp, value
                FROM log
                WHERE seq >= ? ORDER BY seq ASC
`, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		op, err := scanOp(rows, l.encoding)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

type sqlSequencer struct{}

func (*sqlSequencer) GetSequence(tx Transaction) Sequence {
	var u uint64
	if err := tx.Tx().QueryRow("SELECT seq FROM sequence LIMIT 1").Scan(&u); err != nil {
		return 0
	}
	return Sequence(u)
}

func (s *sqlSequencer) GetNextSequence(tx Transaction) Sequence {
	return s.GetSequence(tx) + 1
}

func (*sqlSequencer) SetSequence(tx Transaction, seq Sequence) error {
	_, err := tx.Tx().Exec("UPDATE sequence SET seq = ?", seq)
	return err
}
