package blockdev

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// decodePlist decodes the root value of an Apple XML property list (as
// produced by `ioreg -a -l`) into plain Go values:
//
//	<dict>            -> map[string]any
//	<array>            -> []any
//	<string>           -> string
//	<integer>          -> int64
//	<true/>, <false/>  -> bool
//	<data>             -> string (raw base64 text, whitespace stripped)
//
// Element types outside this set (e.g. <real>, <date>) are skipped
// leniently: the element is consumed but omitted from its parent dict or
// array, rather than causing an error. This keeps the decoder usable
// against real ioreg output, which contains many property types this
// package has no use for.
func decodePlist(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("blockdev: decoding plist: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "plist" {
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("blockdev: decoding plist: %w", err)
			}
			continue
		}
		v, _, err := decodeNextValue(dec)
		if err != nil {
			return nil, fmt.Errorf("blockdev: decoding plist: %w", err)
		}
		return v, nil
	}
}

// decodeNextValue reads and decodes the next element from dec. skip is true
// when the element's type isn't one decodePlist understands, in which case
// the caller (a dict or array) should omit this value entirely.
func decodeNextValue(dec *xml.Decoder) (v any, skip bool, err error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return decodeElement(dec, t)
		case xml.EndElement:
			return nil, true, nil
		}
	}
}

func decodeElement(dec *xml.Decoder, start xml.StartElement) (any, bool, error) {
	switch start.Name.Local {
	case "dict":
		v, err := decodeDict(dec)
		return v, false, err
	case "array":
		v, err := decodeArray(dec)
		return v, false, err
	case "string":
		s, err := decodeCharData(dec)
		return s, false, err
	case "data":
		s, err := decodeCharData(dec)
		return stripWhitespace(s), false, err
	case "integer":
		s, err := decodeCharData(dec)
		if err != nil {
			return nil, false, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parsing <integer>%s</integer>: %w", s, err)
		}
		return n, false, nil
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, false, err
		}
		return start.Name.Local == "true", false, nil
	default:
		// <real>, <date>, or anything else this package has no use for.
		if err := dec.Skip(); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
}

// decodeDict decodes a <dict>'s <key>/value pairs, having already consumed
// the <dict> start tag. Pairs whose value is a skipped element type are
// omitted from the result.
func decodeDict(dec *xml.Decoder) (map[string]any, error) {
	m := map[string]any{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return m, nil
		case xml.StartElement:
			if t.Name.Local != "key" {
				if err := dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			key, err := decodeCharData(dec)
			if err != nil {
				return nil, err
			}
			val, skip, err := decodeNextValue(dec)
			if err != nil {
				return nil, err
			}
			if !skip {
				m[key] = val
			}
		}
	}
}

// decodeArray decodes an <array>'s elements, having already consumed the
// <array> start tag. Elements of a skipped type are omitted.
func decodeArray(dec *xml.Decoder) ([]any, error) {
	var arr []any
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return arr, nil
		case xml.StartElement:
			v, skip, err := decodeElement(dec, t)
			if err != nil {
				return nil, err
			}
			if !skip {
				arr = append(arr, v)
			}
		}
	}
}

// decodeCharData reads and concatenates character data up to the next end
// element, having already consumed the element's start tag.
func decodeCharData(dec *xml.Decoder) (string, error) {
	var buf bytes.Buffer
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf.Write(t)
		case xml.EndElement:
			return buf.String(), nil
		}
	}
}

func stripWhitespace(s string) string {
	return strings.Join(strings.Fields(s), "")
}
