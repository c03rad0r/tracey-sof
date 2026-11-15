package bank

import "sort"

// MergeStatements adds PDF statement sources and incoming match records to an
// existing cache. A confirmed CSV match is never downgraded; the PDF source is
// recorded beside it and its artifact is used when evidence is applied.
func MergeStatements(existing Cache, incoming []MatchRecord, sources []StatementSource) Cache {
	byKey := make(map[string]MatchRecord, len(existing.Records)+len(incoming))
	order := make([]string, 0, len(existing.Records)+len(incoming))
	for _, record := range existing.Records {
		byKey[record.Key()] = record
		order = append(order, record.Key())
	}
	for _, record := range incoming {
		previous, exists := byKey[record.Key()]
		if exists && previous.Status == "matched" {
			if record.Status == "matched" {
				if record.StatementFile != "" {
					previous.StatementFile = record.StatementFile
					previous.StatementPage = record.StatementPage
					previous.StatementSHA256 = record.StatementSHA256
					previous.StatementMonth = record.StatementMonth
					previous.StatementRecipient = record.StatementRecipient
					previous.StatementPurpose = record.StatementPurpose
					previous.StatementText = record.StatementText
					previous.StatementReference = record.StatementReference
					previous.Artifact = record.Artifact
					previous.ArtifactSHA256 = record.ArtifactSHA256
				}
				if record.Confirmed {
					previous.Confirmed = true
				}
			}
			byKey[record.Key()] = previous
			continue
		}
		if !exists {
			order = append(order, record.Key())
		}
		byKey[record.Key()] = record
	}
	records := make([]MatchRecord, 0, len(byKey))
	for _, key := range order {
		if record, exists := byKey[key]; exists {
			records = append(records, record)
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date < records[j].Date
		}
		return records[i].Key() < records[j].Key()
	})
	existing.Records = records
	for _, source := range sources {
		found := false
		for index, current := range existing.Statements {
			if current.SHA256 == source.SHA256 {
				existing.Statements[index] = source
				found = true
				break
			}
		}
		if !found {
			existing.Statements = append(existing.Statements, source)
		}
	}
	sort.SliceStable(existing.Statements, func(i, j int) bool {
		if existing.Statements[i].Period != existing.Statements[j].Period {
			return existing.Statements[i].Period < existing.Statements[j].Period
		}
		return existing.Statements[i].File < existing.Statements[j].File
	})
	return existing
}
