package mailer

// EncodeMIME builds the same validated MIME message for SMTP and Gmail delivery.
func EncodeMIME(from, fromName string, msg Message) ([]byte, error) {
	return (&SMTP{from: from, fromName: fromName}).buildRaw(msg)
}
