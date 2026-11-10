package provenance

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"tracey-sof/internal/model"
)

// Evidence indexes purchase assertions by their exact transaction, outpoint, or
// address scope. Manual confirmations add assertions and manual rejections take
// precedence over both automatic matches and confirmations.
type Evidence struct {
	transactions map[string][]string
	outpoints    map[string][]string
	addresses    map[string][]string
}

type evidenceSets map[string]map[string]struct{}

func (s evidenceSets) add(key, purchaseID string) {
	ids := s[key]
	if ids == nil {
		ids = make(map[string]struct{})
		s[key] = ids
	}
	ids[purchaseID] = struct{}{}
}

func (s evidenceSets) remove(key, purchaseID string) {
	delete(s[key], purchaseID)
}

func freezeEvidenceSets(sets evidenceSets) map[string][]string {
	frozen := make(map[string][]string, len(sets))
	for key, ids := range sets {
		if len(ids) == 0 {
			continue
		}
		values := make([]string, 0, len(ids))
		for id := range ids {
			values = append(values, id)
		}
		sort.Strings(values)
		frozen[key] = values
	}
	return frozen
}

func validEvidenceTxID(txid string) bool {
	if len(txid) != 64 || txid != strings.ToLower(txid) {
		return false
	}
	decoded, err := hex.DecodeString(txid)
	return err == nil && len(decoded) == 32
}

type purchaseLinkTarget struct {
	kind string
	key  string
}

func parsePurchaseLinkTarget(target string) (purchaseLinkTarget, error) {
	if target == "" {
		return purchaseLinkTarget{}, fmt.Errorf("purchase link target is empty")
	}
	if validEvidenceTxID(target) {
		return purchaseLinkTarget{kind: "transaction", key: target}, nil
	}
	if strings.Contains(target, ":") {
		outpoint, err := model.ParseOutpoint(target)
		if err != nil || !validEvidenceTxID(outpoint.TxID) {
			return purchaseLinkTarget{}, fmt.Errorf("invalid purchase-link target %q", target)
		}
		return purchaseLinkTarget{kind: "outpoint", key: outpoint.String()}, nil
	}
	if strings.TrimSpace(target) != target {
		return purchaseLinkTarget{}, fmt.Errorf("invalid purchase-link target %q", target)
	}
	return purchaseLinkTarget{kind: "address", key: target}, nil
}

// NewEvidence constructs an immutable evidence index. Purchase links refer to
// the same scopes as automatic evidence: a bare txid, txid:vout, or an address.
func NewEvidence(purchases model.PurchaseFile, annotations model.AnnotationFile) (*Evidence, error) {
	transactions := make(evidenceSets)
	outpoints := make(evidenceSets)
	addresses := make(evidenceSets)
	purchaseIDs := make(map[string]struct{}, len(purchases.Purchases))

	for _, purchase := range purchases.Purchases {
		if purchase.ID == "" {
			return nil, fmt.Errorf("purchase ID is empty")
		}
		if _, exists := purchaseIDs[purchase.ID]; exists {
			return nil, fmt.Errorf("duplicate purchase ID %q", purchase.ID)
		}
		purchaseIDs[purchase.ID] = struct{}{}
		for _, txid := range purchase.TxIDs {
			transactions.add(txid, purchase.ID)
		}
		for _, outpoint := range purchase.Outpoints {
			outpoints.add(outpoint.String(), purchase.ID)
		}
		for _, address := range purchase.Addresses {
			addresses.add(address, purchase.ID)
		}
	}

	type manualLink struct {
		target     purchaseLinkTarget
		purchaseID string
	}
	confirmed := make([]manualLink, 0, len(annotations.PurchaseLinks))
	rejected := make([]manualLink, 0, len(annotations.PurchaseLinks))
	for _, link := range annotations.PurchaseLinks {
		if link.Decision != "confirmed" && link.Decision != "rejected" {
			return nil, fmt.Errorf("purchase link for %q has invalid decision %q", link.Target, link.Decision)
		}
		if _, exists := purchaseIDs[link.PurchaseID]; !exists {
			return nil, fmt.Errorf("purchase link for %q refers to unknown purchase ID %q", link.Target, link.PurchaseID)
		}
		target, err := parsePurchaseLinkTarget(link.Target)
		if err != nil {
			return nil, err
		}
		item := manualLink{target: target, purchaseID: link.PurchaseID}
		if link.Decision == "confirmed" {
			confirmed = append(confirmed, item)
		} else {
			rejected = append(rejected, item)
		}
	}

	scopes := map[string]evidenceSets{
		"transaction": transactions,
		"outpoint":    outpoints,
		"address":     addresses,
	}
	apply := func(link manualLink, remove bool) {
		sets := scopes[link.target.kind]
		if remove {
			sets.remove(link.target.key, link.purchaseID)
		} else {
			sets.add(link.target.key, link.purchaseID)
		}
	}
	for _, link := range confirmed {
		apply(link, false)
	}
	for _, link := range rejected {
		apply(link, true)
	}

	return &Evidence{
		transactions: freezeEvidenceSets(transactions),
		outpoints:    freezeEvidenceSets(outpoints),
		addresses:    freezeEvidenceSets(addresses),
	}, nil
}

func evidenceIDs(index map[string][]string, key string) []string {
	ids := index[key]
	return append([]string(nil), ids...)
}

// TransactionIDs returns the sorted purchase IDs asserted for txid.
func (e *Evidence) TransactionIDs(txid string) []string {
	if e == nil {
		return nil
	}
	return evidenceIDs(e.transactions, txid)
}

// OutpointIDs returns the sorted purchase IDs asserted for one exact outpoint.
func (e *Evidence) OutpointIDs(outpoint model.Outpoint) []string {
	if e == nil {
		return nil
	}
	return evidenceIDs(e.outpoints, outpoint.String())
}

// AddressIDs returns the sorted purchase IDs asserted for an exact address.
func (e *Evidence) AddressIDs(address string) []string {
	if e == nil {
		return nil
	}
	return evidenceIDs(e.addresses, address)
}
