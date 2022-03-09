//go:build go1.18
// +build go1.18

package bindnode_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/node/bindnode"
	"github.com/ipld/go-ipld-prime/schema"
	schemadmt "github.com/ipld/go-ipld-prime/schema/dmt"
	schemadsl "github.com/ipld/go-ipld-prime/schema/dsl"
)

var fuzzInputs = []struct {
	schemaDSL, nodeDagJSON string
}{
	{
		schemaDSL:   `type Root bool`,
		nodeDagJSON: `true`,
	},
	{
		schemaDSL:   `type Root int`,
		nodeDagJSON: `123`,
	},
	{
		schemaDSL:   `type Root float`,
		nodeDagJSON: `45.67`,
	},
	{
		schemaDSL:   `type Root string`,
		nodeDagJSON: `"foo"`,
	},
	{
		schemaDSL:   `type Root bytes`,
		nodeDagJSON: `{"/":{"bytes":"ZGVhZGJlZWY"}}`,
	},
	{
		schemaDSL:   `type Root [Int]`,
		nodeDagJSON: `[3,2,1]`,
	},
	{
		schemaDSL:   `type Root [String]`,
		nodeDagJSON: `["x","y","z"]`,
	},
	{
		schemaDSL:   `type Root {String:Int}`,
		nodeDagJSON: `{"a":20,"b":10}`,
	},
	{
		schemaDSL:   `type Root {String:Float}`,
		nodeDagJSON: `{"a":20.5,"b":10.2}`,
	},
	{
		schemaDSL: `type Root struct {
			F1 Bool
			F2 Bytes
		}`,
		nodeDagJSON: `{"F1":true,"F2":{"/":{"bytes":"ZGVhZGJlZWY"}}}`,
	},
	{
		schemaDSL: `type Root struct {
			F1 Int
			F2 Float
		} representation tuple`,
		nodeDagJSON: `[23,45.67]`,
	},
	{
		schemaDSL: `type Root enum {
			| aa ("a")
			| bb ("b")
		} representation string`,
		nodeDagJSON: `"b"`,
	},
	{
		schemaDSL: `type Root enum {
			| One ("1")
			| Two ("2")
		} representation int`,
		nodeDagJSON: `"Two"`,
	},
	{
		schemaDSL: `type Root union {
			| Int    "x"
			| String "y"
		} representation keyed`,
		nodeDagJSON: `{"y":"foo"}`,
	},
}

func marshalDagCBOR(tb testing.TB, node datamodel.Node) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := dagcbor.Encode(node, &buf); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func marshalDagJSON(tb testing.TB, node datamodel.Node) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := dagjson.Encode(node, &buf); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// TODO: consider allowing any codec multicode instead of hard-coding dagcbor

// TODO: we always infer the Go type; it would be interesting to also support
// inferring the IPLD schema, or to supply both.

// TODO: encoding roundtrips via codecs are a good way to exercise bindnode's
// Node implementation, but they do not call all the methods on the Node
// interface. Consider other ways to call the rest of the methods, akin to how
// infer_test.go has useNodeAsKind.

func FuzzBindnodeViaDagCBOR(f *testing.F) {
	for _, input := range fuzzInputs {
		schemaDMT, err := schemadsl.ParseBytes([]byte(input.schemaDSL))
		if err != nil {
			f.Fatal(err)
		}
		schemaNode := bindnode.Wrap(schemaDMT, schemadmt.Type.Schema.Type())
		schemaDagCBOR := marshalDagCBOR(f, schemaNode.Representation())

		nodeBuilder := basicnode.Prototype.Any.NewBuilder()
		if err := dagjson.Decode(nodeBuilder, strings.NewReader(input.nodeDagJSON)); err != nil {
			f.Fatal(err)
		}
		node := nodeBuilder.Build()
		nodeDagCBOR := marshalDagCBOR(f, node)
		f.Add(schemaDagCBOR, nodeDagCBOR)

		// TODO: verify that nodeDagCBOR actually fits the schema.
		// Otherwise, if any of our fuzz inputs are wrong, we might not notice.
	}
	f.Fuzz(func(t *testing.T, schemaDagCBOR, nodeDagCBOR []byte) {
		schemaBuilder := schemadmt.Type.Schema.Representation().NewBuilder()

		if err := dagcbor.Decode(schemaBuilder, bytes.NewReader(schemaDagCBOR)); err != nil {
			t.Skipf("invalid schema-schema dag-cbor: %v", err)
		}

		schemaNode := schemaBuilder.Build().(schema.TypedNode)
		schemaDMT := bindnode.Unwrap(schemaNode).(*schemadmt.Schema)

		// Log the input schema and node we're fuzzing with, to help debugging.
		// We also use dag-json, as it's more human readable.
		t.Logf("schema in dag-cbor: %x", schemaDagCBOR)
		t.Logf("node in dag-cbor: %x", nodeDagCBOR)
		t.Logf("schema in dag-json: %s", marshalDagJSON(t, schemaNode.Representation()))
		{
			nodeBuilder := basicnode.Prototype.Any.NewBuilder()
			if err := dagcbor.Decode(nodeBuilder, bytes.NewReader(nodeDagCBOR)); err != nil {
				// If some dag-cbor bytes don't decode into the Any prototype,
				// then they're just not valid dag-cbor at all.
				t.Skipf("invalid node dag-cbor: %v", err)
			}
			node := nodeBuilder.Build()
			t.Logf("node in dag-json: %s", marshalDagJSON(t, node))
		}

		ts := new(schema.TypeSystem)
		ts.Init()
		// For the time being, we're not interested in panics from
		// schemadmt.Compile or schema.TypeSystem. They are relatively prone to
		// panics at the moment, and right now we're mainly interested in bugs
		// in bindnode and dagcbor.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("invalid schema: %v", r)
				}
			}()
			schemadmt.Compile(ts, schemaDMT)
		}()

		schemaType := ts.TypeByName("Root")
		if schemaType == nil {
			t.Skipf("schema has no Root type")
		}
		var proto schema.TypedPrototype
		func() {
			defer func() {
				if r := recover(); r != nil {
					str := fmt.Sprint(r)
					switch {
					case strings.Contains(str, "bindnode: unexpected nil schema.Type"):
					case strings.Contains(str, "is not a valid Go identifier"):
					default:
						panic(r)
					}
					t.Skipf("invalid schema: %v", r)
				}
			}()
			proto = bindnode.Prototype(nil, schemaType)
		}()

		for _, repr := range []bool{false, true} {
			t.Logf("decode and encode roundtrip with dag-cbor repr=%v", repr)
			var nodeBuilder datamodel.NodeBuilder
			if !repr {
				nodeBuilder = proto.NewBuilder()
			} else {
				nodeBuilder = proto.Representation().NewBuilder()
			}
			if err := dagcbor.Decode(nodeBuilder, bytes.NewReader(nodeDagCBOR)); err != nil {
				// The dag-cbor isn't valid for this node. Nothing else to do.
				// We don't use t.Skip, because a dag-cbor might only be valid
				// at the repr level, but not at the type level.
				continue
			}
			node := nodeBuilder.Build()
			if repr {
				node = node.(schema.TypedNode).Representation()
			}
			// Unwrap returns a pointer, and %#v prints pointers as hex,
			// so to get useful output, use reflect to dereference them.
			t.Logf("decode successful: %#v", reflect.ValueOf(bindnode.Unwrap(node)).Elem().Interface())
			reenc := marshalDagCBOR(t, node)
			if !bytes.Equal(reenc, nodeDagCBOR) {
				// TODO: reenable this once the dagcbor codec rejects non-canonical
				// inputs like "00" and "a000", "0" and "a0" respectively.
				// t.Errorf("node reencoded as %q rather than %q", reenc, nodeDagCBOR)
			}
			t.Logf("re-encode successful: %q", reenc)
		}
	})
}