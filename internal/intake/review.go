package intake

import (
	"fmt"
	"sort"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

type ReviewOptions struct {
	Accept     string
	Reject     string
	Pending    string
	Role       string
	Note       string
	PurchaseID string
	Date       string
	Provider   string
	AmountSats *int64
	Fiat       *model.Fiat
}

func ApplyReview(file *File, options ReviewOptions) error {
	actions := 0
	for _, value := range []string{options.Accept, options.Reject, options.Pending} {
		if value != "" {
			actions++
		}
	}
	if actions > 1 {
		return fmt.Errorf("choose only one of accept, reject, or pending")
	}
	var source *Source
	var candidate *Candidate
	if options.Accept != "" {
		source, candidate = file.CandidateByID(options.Accept)
	} else if options.Reject != "" {
		source, candidate = file.CandidateByID(options.Reject)
	} else if options.Pending != "" {
		source, candidate = file.CandidateByID(options.Pending)
	}
	if actions == 1 && candidate == nil {
		return fmt.Errorf("candidate not found")
	}
	if candidate != nil {
		if options.Role != "" {
			if err := ValidateRoleForKind(candidate.Kind, options.Role); err != nil {
				return err
			}
			candidate.Role = options.Role
		}
		if options.Note != "" {
			candidate.Review.Note = options.Note
		}
		switch {
		case options.Accept != "":
			if !PromotableRole(candidate.Role) {
				return fmt.Errorf("candidate %s has non-promotable role %q; set a received_at, purchase_txid, or purchase_outpoint role first", candidate.ID, candidate.Role)
			}
			candidate.Review.Decision = DecisionAccepted
		case options.Reject != "":
			candidate.Review.Decision = DecisionRejected
		case options.Pending != "":
			candidate.Review.Decision = DecisionPending
		}
	}
	if options.PurchaseID != "" || options.Date != "" || options.Provider != "" || options.AmountSats != nil || options.Fiat != nil {
		if source == nil {
			if len(file.Sources) != 1 {
				return fmt.Errorf("purchase metadata edits require a candidate action or a single-source intake file")
			}
			source = &file.Sources[0]
		}
		if options.PurchaseID != "" {
			source.Purchase.ID = options.PurchaseID
		}
		if options.Date != "" {
			source.Purchase.Date = options.Date
		}
		if options.Provider != "" {
			source.Purchase.Provider = options.Provider
		}
		if options.AmountSats != nil {
			source.Purchase.AmountSats = options.AmountSats
		}
		if options.Fiat != nil {
			source.Purchase.Fiat = options.Fiat
		}
		if err := ValidateProposal(source.Purchase); err != nil {
			return err
		}
	}
	if actions == 0 && options.Role == "" && options.Note == "" && options.PurchaseID == "" && options.Date == "" && options.Provider == "" && options.AmountSats == nil && options.Fiat == nil {
		return fmt.Errorf("no review action was supplied")
	}
	return Validate(*file)
}

func List(file File) []string {
	var lines []string
	for _, source := range file.Sources {
		for _, candidate := range source.Candidates {
			line := fmt.Sprintf("%s\t%s\t%s\trole=%s\tconfidence=%s\t%s", candidate.ID, candidate.Kind, candidate.Value, candidate.Role, candidate.Confidence, candidate.Review.Decision)
			if candidate.Resolution != nil {
				if len(candidate.Resolution.CurrentUTXOs) > 0 {
					line += fmt.Sprintf("\tcurrent_utxos=%d", len(candidate.Resolution.CurrentUTXOs))
				}
				if candidate.Resolution.TxIDExists {
					line += "\ttxid_exists=true"
				}
				if candidate.Resolution.OutpointExists {
					line += "\toutpoint_exists=true"
				}
				if candidate.Resolution.InTrace {
					line += "\tin_trace=true"
				}
			}
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines
}

func Promote(file File, existing model.PurchaseFile, baseDir string, replace bool) (model.PurchaseFile, error) {
	result := model.PurchaseFile{SchemaVersion: model.SchemaVersion, Purchases: append([]model.Purchase(nil), existing.Purchases...)}
	index := map[string]int{}
	for i, purchase := range result.Purchases {
		index[purchase.ID] = i
	}
	promotedCount := 0
	for _, source := range file.Sources {
		var accepted []Candidate
		for _, candidate := range source.Candidates {
			if candidate.Review.Decision != DecisionAccepted {
				continue
			}
			if !PromotableRole(candidate.Role) {
				return result, fmt.Errorf("accepted candidate %s has non-promotable role %q", candidate.ID, candidate.Role)
			}
			accepted = append(accepted, candidate)
		}
		if len(accepted) == 0 {
			continue
		}
		if source.Purchase.ID == "" {
			return result, fmt.Errorf("source %s has no purchase ID", source.Path)
		}
		purchase := model.Purchase{
			ID:         source.Purchase.ID,
			Date:       source.Purchase.Date,
			Provider:   source.Purchase.Provider,
			Notes:      source.Purchase.Notes,
			AmountSats: source.Purchase.AmountSats,
			Fiat:       source.Purchase.Fiat,
			Artifacts:  []model.Artifact{{Path: source.Path, SHA256: source.SHA256, Description: "Scanned or imported purchase evidence"}},
		}
		for _, candidate := range accepted {
			switch candidate.Role {
			case RoleReceivedAt:
				purchase.Addresses = append(purchase.Addresses, candidate.Value)
			case RolePurchaseTxID:
				purchase.TxIDs = append(purchase.TxIDs, candidate.Value)
			case RolePurchaseOutpoint:
				outpoint, err := model.ParseOutpoint(candidate.Value)
				if err != nil {
					return result, err
				}
				purchase.Outpoints = append(purchase.Outpoints, outpoint)
			}
		}
		purchase.Addresses = uniqueStrings(purchase.Addresses)
		purchase.TxIDs = uniqueStrings(purchase.TxIDs)
		purchase.Outpoints = uniqueOutpoints(purchase.Outpoints)
		if len(purchase.Addresses)+len(purchase.TxIDs)+len(purchase.Outpoints) == 0 {
			return result, fmt.Errorf("source %s has no accepted evidence values", source.Path)
		}
		if position, exists := index[purchase.ID]; exists {
			if !replace {
				return result, fmt.Errorf("purchase ID %q already exists; choose another ID or pass --replace", purchase.ID)
			}
			result.Purchases[position] = purchase
			promotedCount++
			continue
		}
		index[purchase.ID] = len(result.Purchases)
		result.Purchases = append(result.Purchases, purchase)
		promotedCount++
	}
	if promotedCount == 0 {
		return result, fmt.Errorf("no accepted intake candidates were available for promotion")
	}
	if err := input.ValidatePurchases(result, baseDir, true); err != nil {
		return result, err
	}
	return result, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueOutpoints(values []model.Outpoint) []model.Outpoint {
	seen := map[string]bool{}
	result := make([]model.Outpoint, 0, len(values))
	for _, value := range values {
		if !seen[value.String()] {
			seen[value.String()] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func Summary(file File) string {
	var sources, candidates, accepted, rejected, pending int
	for _, source := range file.Sources {
		sources++
		for _, candidate := range source.Candidates {
			candidates++
			switch candidate.Review.Decision {
			case DecisionAccepted:
				accepted++
			case DecisionRejected:
				rejected++
			default:
				pending++
			}
		}
	}
	return fmt.Sprintf("%d sources, %d candidates (%d accepted, %d rejected, %d pending)", sources, candidates, accepted, rejected, pending)
}
