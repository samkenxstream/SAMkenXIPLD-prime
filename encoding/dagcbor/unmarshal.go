package dagcbor

import (
	"fmt"
	"math"

	cid "github.com/ipfs/go-cid"
	"github.com/polydawn/refmt/shared"
	"github.com/polydawn/refmt/tok"

	ipld "github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
)

// This should be identical to the general feature in the parent package,
// except for the `case tok.TBytes` block,
// which has dag-cbor's special sauce for detecting schemafree links.

func Unmarshal(nb ipld.NodeBuilder, tokSrc shared.TokenSource) (ipld.Node, error) {
	var tk tok.Token
	done, err := tokSrc.Step(&tk)
	if done || err != nil {
		return nil, err
	}
	return unmarshal(nb, tokSrc, &tk)
}

// starts with the first token already primed.  Necessary to get recursion
//  to flow right without a peek+unpeek system.
func unmarshal(nb ipld.NodeBuilder, tokSrc shared.TokenSource, tk *tok.Token) (ipld.Node, error) {
	// FUTURE: check for typed.NodeBuilder that's going to parse a Link (they can slurp any token kind they want).
	switch tk.Type {
	case tok.TMapOpen:
		mb, err := nb.CreateMap()
		if err != nil {
			return nil, err
		}
		expectLen := tk.Length
		if tk.Length == -1 {
			expectLen = math.MaxInt32
		}
		observedLen := 0
		var k string
		var v ipld.Node
		for {
			_, err := tokSrc.Step(tk)
			if err != nil {
				return nil, err
			}
			switch tk.Type {
			case tok.TMapClose:
				if expectLen != math.MaxInt32 && observedLen != expectLen {
					return nil, fmt.Errorf("unexpected mapClose before declared length")
				}
				return mb.Build()
			case tok.TString:
				// continue
			default:
				return nil, fmt.Errorf("unexpected %s token while expecting map key", tk.Type)
			}
			observedLen++
			if observedLen > expectLen {
				return nil, fmt.Errorf("unexpected continuation of map elements beyond declared length")
			}
			k = tk.Str
			// FUTURE: check for typed.NodeBuilder; need to specialize before recursing if so.
			v, err = Unmarshal(nb, tokSrc)
			if err != nil {
				return nil, err
			}
			kn, err := nb.CreateString(k)
			if err != nil {
				panic(err) // TODO: I'm no longer sure Insert should take a Node instead of string, but not recursing into reviewing that choice now.
			}
			if err := mb.Insert(kn, v); err != nil {
				return nil, err
			}
		}
	case tok.TMapClose:
		return nil, fmt.Errorf("unexpected mapClose token")
	case tok.TArrOpen:
		lb, err := nb.CreateList()
		if err != nil {
			return nil, err
		}
		expectLen := tk.Length
		if tk.Length == -1 {
			expectLen = math.MaxInt32
		}
		observedLen := 0
		for {
			_, err := tokSrc.Step(tk)
			if err != nil {
				return nil, err
			}
			switch tk.Type {
			case tok.TArrClose:
				if expectLen != math.MaxInt32 && observedLen != expectLen {
					return nil, fmt.Errorf("unexpected arrClose before declared length")
				}
				return lb.Build()
			default:
				observedLen++
				if observedLen > expectLen {
					return nil, fmt.Errorf("unexpected continuation of array elements beyond declared length")
				}
				// FUTURE: check for typed.NodeBuilder; need to specialize before recursing if so.
				//  N.B. when considering optionals for tuple-represented structs, keep in mind how murky that will get here.
				v, err := unmarshal(nb, tokSrc, tk)
				if err != nil {
					return nil, err
				}
				lb.Append(v)
			}
		}
	case tok.TArrClose:
		return nil, fmt.Errorf("unexpected arrClose token")
	case tok.TNull:
		return nb.CreateNull()
	case tok.TString:
		return nb.CreateString(tk.Str)
	case tok.TBytes:
		if !tk.Tagged {
			return nb.CreateBytes(tk.Bytes)
		}
		switch tk.Tag {
		case linkTag:
			elCid, err := cid.Cast(tk.Bytes)
			if err != nil {
				return nil, err
			}
			return nb.CreateLink(cidlink.Link{elCid})
		default:
			return nil, fmt.Errorf("unhandled cbor tag %d", tk.Tag)
		}
	case tok.TBool:
		return nb.CreateBool(tk.Bool)
	case tok.TInt:
		return nb.CreateInt(int(tk.Int)) // FIXME overflow check
	case tok.TUint:
		return nb.CreateInt(int(tk.Uint)) // FIXME overflow check
	case tok.TFloat64:
		return nb.CreateFloat(tk.Float64)
	default:
		panic("unreachable")
	}
}
