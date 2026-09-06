package contract

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"
)

// The `borg` struct tag, which is one convention two packages read: what a
// contract file's field says about the element, and what a consumer's mirror
// says about the same field with a word of its own. Two packages parsing one tag
// two ways would be two spellings of one convention, so the words are read here
// and [TagWords] is what package consumercontract calls.

// readTag is what a field's `borg` tag says about the element: the facts the diff
// reads off it. A tag naming anything else is ignored rather than refused — this
// derivation reads seven words and a build may carry tags for other tools on the
// same field.
func readTag(tag *ast.BasicLit) (Element, error) {
	var e Element
	if tag == nil {
		return e, nil
	}
	for _, word := range TagWords(tag.Value) {
		name, argument, _ := strings.Cut(word, "=")
		switch name {
		case TagPopulated:
			e.Populated = true
		case TagRequired:
			e.Required = true
		case TagDeprecated:
			e.Marked = true
		case TagNotNull:
			e.NotNull = true
		case TagUnique:
			e.Unique = true
		case TagDomain:
			e.Domain = DomainNames(argument)
			if len(e.Domain) == 0 {
				return Element{}, fmt.Errorf("the domain %q names nothing", argument)
			}
		case TagRange:
			r, err := ParseRange(argument)
			if err != nil {
				return Element{}, err
			}
			e.Range = r
		}
	}
	return e, nil
}

// ParseRange is the range a tag or a stored argument holds, written low..high.
func ParseRange(text string) (*Range, error) {
	low, high, found := strings.Cut(text, "..")
	if !found {
		return nil, fmt.Errorf("the range %q is not written low..high", text)
	}
	from, err := strconv.ParseFloat(strings.TrimSpace(low), 64)
	if err != nil {
		return nil, fmt.Errorf("the low end of %q is not a number", text)
	}
	to, err := strconv.ParseFloat(strings.TrimSpace(high), 64)
	if err != nil {
		return nil, fmt.Errorf("the high end of %q is not a number", text)
	}
	if from > to {
		return nil, fmt.Errorf("the range %q has its ends the wrong way round", text)
	}
	return &Range{Low: from, High: to}, nil
}

// TagWords is the comma-separated words of one field's `borg` tag, and none where
// the field has no tag or no `borg` key. It is exported because package consumer
// contract reads the same tag on a consumer's mirror, with a word of its own, and
// two packages parsing one tag two ways would be two spellings of one convention.
func TagWords(quoted string) []string {
	if quoted == "" {
		return nil
	}
	literal, err := strconv.Unquote(quoted)
	if err != nil {
		return nil
	}
	value, ok := reflectTagLookup(literal, tagKey)
	if !ok {
		return nil
	}
	var words []string
	for _, word := range strings.Split(value, ",") {
		if word = strings.TrimSpace(word); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// reflectTagLookup is the one value of a struct tag, read the way the language
// defines a tag rather than by asking the reflect package: a space-separated list
// of key:"value" pairs. It is written out here because reading a tag through
// reflect.StructTag would mean holding a type at run time, and what this has is
// the tag's text out of the source.
func reflectTagLookup(tag, key string) (string, bool) {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' {
			i++
		}
		if i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		quoted := tag[:i+1]
		tag = tag[i+1:]
		if name != key {
			continue
		}
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return "", false
		}
		return value, true
	}
	return "", false
}
