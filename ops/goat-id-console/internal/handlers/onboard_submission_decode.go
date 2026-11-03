package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// How a candidate's answers decode, on the two paths that read them.
//
// At submit the decode is strict: a field of the wrong JSON type is refused
// with a 400 that names the field, so a caller learns about a mistake while
// it can still fix it and the invite is not consumed. A stored intake record
// is read tolerantly: rows accepted before submit refused wrong types still
// have to complete, and a wrong-typed field there is skipped, the rest kept.

// decodeSubmittedAnswers is the submit path's decoder. A *json.UnmarshalTypeError
// comes back as-is so the caller can name the field.
func decodeSubmittedAnswers(raw json.RawMessage) (candidateSubmission, error) {
	var sub candidateSubmission
	if len(raw) == 0 {
		return sub, nil
	}
	return sub, json.Unmarshal(raw, &sub)
}

// decodeCandidateSubmission reads a stored candidate's answers: a field of the
// wrong JSON type is skipped and the rest is kept, so an intake record stored
// before submit refused such fields decodes at completion the way it did when
// it was accepted. Only JSON that does not parse is an error.
func decodeCandidateSubmission(raw json.RawMessage) (candidateSubmission, error) {
	var sub candidateSubmission
	if len(raw) == 0 {
		return sub, nil
	}
	err := json.Unmarshal(raw, &sub)
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return sub, nil
	}
	return sub, err
}

// wrongTypeMessage words a type refusal for the caller: the field by its JSON
// name, the type it takes, and the type that arrived.
func wrongTypeMessage(e *json.UnmarshalTypeError) string {
	field := e.Field
	if field == "" {
		field = "submission"
	}
	return fmt.Sprintf("%s must be %s, not %s", field, jsonTypeName(e.Type), e.Value)
}

func jsonTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "text"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "an object"
	default:
		return t.String()
	}
}
