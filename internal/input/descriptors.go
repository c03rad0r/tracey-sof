package input

import (
	"fmt"
	"strings"

	"tracey-sof/internal/model"
)

// ScanObject is the descriptor plus optional derivation range accepted by Core.
type ScanObject struct {
	ID         string
	Label      string
	Descriptor string
	Range      *[2]uint32
}

func ScanObjects(owned model.Owned) ([]ScanObject, error) {
	var objects []ScanObject
	for i, item := range owned.Addresses {
		objects = append(objects, ScanObject{ID: fmt.Sprintf("address:%d", i), Label: item.Label, Descriptor: "addr(" + item.Address + ")"})
	}
	for _, item := range owned.Descriptors {
		objects = append(objects, ScanObject{ID: "descriptor:" + item.ID, Label: item.Label, Descriptor: item.Descriptor, Range: item.Range})
	}
	for _, item := range owned.Xpubs {
		for _, branch := range item.Branches {
			r := item.Range
			var key string
			switch item.ScriptType {
			case "pkh":
				key = "pkh(" + item.Xpub + "/" + fmt.Sprint(branch) + "/*)"
			case "sh(wpkh)":
				key = "sh(wpkh(" + item.Xpub + "/" + fmt.Sprint(branch) + "/*))"
			case "wpkh":
				key = "wpkh(" + item.Xpub + "/" + fmt.Sprint(branch) + "/*)"
			case "tr":
				key = "tr(" + item.Xpub + "/" + fmt.Sprint(branch) + "/*)"
			default:
				return nil, fmt.Errorf("xpub %q has unsupported script type %q", item.ID, item.ScriptType)
			}
			objects = append(objects, ScanObject{ID: fmt.Sprintf("xpub:%s/%d", item.ID, branch), Label: item.Label, Descriptor: key, Range: &r})
		}
	}
	return objects, nil
}

func DescriptorHasRange(descriptor string) bool { return strings.Contains(descriptor, "*") }
