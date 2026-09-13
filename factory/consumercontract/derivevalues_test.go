package consumercontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
)

func TestWritesAreDerivedFromValuesInsteadOfMirrorTags(t *testing.T) {
	mirror := "package main\ntype Row struct { State string `borg:\"populated,domain=old\"`; Amount float64 `borg:\"range=0..1\"` }\n"
	for name, body := range map[string]string{
		"literal":     `row := Row{State: "new", Amount: 90}; _ = row`,
		"assignments": `var row Row; row.State = "new"; row.Amount = 90`,
	} {
		t.Run(name, func(t *testing.T) {
			d, got := derived(t, checkout(t, map[string]string{"consumes.txt": "rows self rows store\n", consumercontract.FileName("rows"): mirror, "main.go": "package main\nfunc main(){" + body + "}"}))
			if d.Partial() || d.CouldNotDerive() {
				t.Fatalf("literal assignments: %s %v", d.Describe(), d.Unfollowed)
			}
			if got["Row.State/sent_domain"] != "new" || got["Row.Amount/sent_range"] != "90..90" {
				t.Fatalf("assertions copied mirror tags: %v", got)
			}
			if _, ok := got["Row.State/populated"]; !ok {
				t.Fatalf("literal population absent: %v", got)
			}
		})
	}
}

func TestUnknownWriteValuesArePartialAndDoNotInventBounds(t *testing.T) {
	d, got := derived(t, checkout(t, map[string]string{
		"consumes.txt":                    "rows self rows store\n",
		consumercontract.FileName("rows"): "package main\ntype Row struct { State string `borg:\"populated,domain=old\"` }",
		"main.go": `package main
func write(value string) { row := Row{State: value}; _ = row }
func main(){}`,
	}))
	if !d.Partial() {
		t.Fatalf("unknown value was complete: %v", got)
	}
	for _, kind := range []string{"populated", "sent_domain", "sent_range"} {
		if _, ok := got["Row.State/"+kind]; ok {
			t.Fatalf("invented %s: %v", kind, got)
		}
	}
	if got["Row.State/sent"] != consumercontract.Sent {
		t.Fatalf("lost observed write: %v", got)
	}
}

func TestAnEmptyStoreWriteDoesNotAssertPopulated(t *testing.T) {
	_, got := derived(t, checkout(t, map[string]string{
		"consumes.txt":                    "rows self rows store\n",
		consumercontract.FileName("rows"): "package main\ntype Row struct { State string `borg:\"populated\"` }",
		"main.go": `package main
func main(){ row:=Row{State:""}; _=row }`,
	}))
	if _, ok := got["Row.State/populated"]; ok {
		t.Fatalf("empty write asserted populated: %v", got)
	}
}

func TestShadowedReceiverDoesNotInventAMirrorRead(t *testing.T) {
	for name, body := range map[string]string{
		"inner literal":          `func main(){ h:=Health{}; { h:=struct{Status string}{}; _=h.Status }; _=h }`,
		"var declaration":        `func main(){ h:=Health{}; { var h struct{Status string}; _=h.Status }; _=h }`,
		"if initializer":         `func main(){ h:=Health{}; if h:=struct{Status string}{}; true { _=h.Status }; _=h }`,
		"interface reassignment": `func main(){ var h any=Health{}; h=untraced(); _=h.Status }; func untraced() any{return nil}`,
	} {
		t.Run(name, func(t *testing.T) {
			d, got := derived(t, checkout(t, map[string]string{"consumes.txt": "health producer health\n", consumercontract.FileName("health"): mirror, "main.go": "package main\n" + body}))
			if _, ok := got["Health.Status/read"]; ok {
				t.Fatalf("unrelated read attributed to mirror: %v", got)
			}
			if !d.Partial() {
				t.Fatalf("unknown receiver not reported: %s", d.Describe())
			}
		})
	}
}

func TestReflectionAndConfigurationAliasesAreRecorded(t *testing.T) {
	for name, code := range map[string]string{
		"reflection": `package main
import r "reflect"
func main(){_ = r.TypeOf(1)}`,
		"mapping": `package main
import env "os"
func read(data map[string]string){_ = data[env.Getenv("FIELD")]}
func main(){}`,
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := derived(t, checkout(t, map[string]string{"main.go": code}))
			if !d.Partial() {
				t.Fatalf("aliased %s was complete", name)
			}
		})
	}
}

func TestStaleAddressAssignmentsDoNotInventDirectCalls(t *testing.T) {
	d, _ := derived(t, checkout(t, map[string]string{"main.go": `package main
import env "os"
import "fmt"
func main(){value:=env.Getenv("ADDRESS"); value="hello"; fmt.Println(value)}`}))
	if d.CouldNotDerive() {
		t.Fatalf("old configuration provenance survived assignment: %s", d.Describe())
	}
}

func TestInvalidAndNestedSourceCannotDeriveCompleteEmpty(t *testing.T) {
	dir := checkout(t, map[string]string{"main.go": "package main\nfunc main(){"})
	d, _ := derived(t, dir)
	if !d.CouldNotDerive() || !strings.Contains(d.Reported, "does not parse") {
		t.Fatalf("parse error: %s", d.Describe())
	}
	dir = checkout(t, map[string]string{"main.go": "package main\nfunc main(){}"})
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "client.go"), []byte("package sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d, _ = derived(t, dir)
	if !d.CouldNotDerive() || !strings.Contains(d.Reported, "root-only") {
		t.Fatalf("nested source: %s", d.Describe())
	}
}

func TestIncrementedStoreValueDoesNotRetainLiteralBounds(t *testing.T) {
	d, got := derived(t, checkout(t, map[string]string{
		"consumes.txt":                    "rows self rows store\n",
		consumercontract.FileName("rows"): "package main\ntype Row struct { Amount int }",
		"main.go": `package main
func main(){ row:=Row{Amount:1}; for i:=0;i<10;i++ {row.Amount++}; _=row }`,
	}))
	if !d.Partial() {
		t.Fatal("loop mutation was complete")
	}
	if _, ok := got["Row.Amount/sent_range"]; ok {
		t.Fatalf("loop mutation retained initializer bounds: %v", got)
	}
}
