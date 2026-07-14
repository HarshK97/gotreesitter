package benchfixtures

import (
	"fmt"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// DigestGoTree computes the shared deep structural digest for a gotreesitter
// tree. The static C oracle implements the same DeepTreeDigestVersion stream.
func DigestGoTree(root *gotreesitter.Node, lang *gotreesitter.Language) (string, error) {
	if root == nil || lang == nil {
		return "", fmt.Errorf("deep tree digest requires a root and language")
	}
	digest := NewDeepTreeDigest()
	if err := addGoTreeDigestNode(digest, root, lang, ""); err != nil {
		return "", err
	}
	return digest.Hex()
}

func addGoTreeDigestNode(digest *DeepTreeDigest, node *gotreesitter.Node, lang *gotreesitter.Language, field string) error {
	if node == nil {
		return fmt.Errorf("deep tree digest encountered a nil Go node")
	}
	start := node.StartPoint()
	end := node.EndPoint()
	typeName := node.Type(lang)
	if typeName == "\\0" {
		typeName = ""
	}
	if err := digest.Add(DeepNode{
		Type:        typeName,
		Field:       field,
		StartByte:   node.StartByte(),
		EndByte:     node.EndByte(),
		StartRow:    start.Row,
		StartColumn: start.Column,
		EndRow:      end.Row,
		EndColumn:   end.Column,
		Named:       node.IsNamed(),
		Extra:       node.IsExtra(),
		Missing:     node.IsMissing(),
		Error:       node.IsError(),
		HasError:    node.HasError(),
		ChildCount:  uint32(node.ChildCount()),
	}); err != nil {
		return err
	}
	for i := 0; i < node.ChildCount(); i++ {
		if err := addGoTreeDigestNode(digest, node.Child(i), lang, node.FieldNameForChild(i, lang)); err != nil {
			return err
		}
	}
	return nil
}
