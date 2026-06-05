package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const maxProposalBytes = 128 * 1024

type EditProposalInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Summary string `json:"summary,omitempty"`
}

type EditProposalReviewInput struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type EditProposal struct {
	ID           string     `json:"id"`
	Path         string     `json:"path"`
	Summary      string     `json:"summary,omitempty"`
	Status       string     `json:"status"`
	ReviewDetail string     `json:"reviewDetail,omitempty"`
	BaseHash     string     `json:"-"`
	Content      string     `json:"-"`
	Diff         []DiffLine `json:"diff"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReviewedAt   *time.Time `json:"reviewedAt,omitempty"`
	AppliedAt    *time.Time `json:"appliedAt,omitempty"`
}

type DiffLine struct {
	Type    string `json:"type"`
	OldLine int    `json:"oldLine,omitempty"`
	NewLine int    `json:"newLine,omitempty"`
	Text    string `json:"text"`
}

func (r *Runtime) ListEditProposals(context.Context) []EditProposal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.editProposals) == 0 {
		return []EditProposal{}
	}
	return append([]EditProposal(nil), r.editProposals...)
}

func (r *Runtime) ProposeEdit(ctx context.Context, input EditProposalInput) (EditProposal, error) {
	if len([]byte(input.Content)) > maxProposalBytes {
		return EditProposal{}, errors.New("proposed content is too large")
	}
	if !utf8.ValidString(input.Content) {
		return EditProposal{}, errors.New("proposed content is not text")
	}
	fullPath, displayPath, err := r.workspacePath(input.Path)
	if err != nil {
		return EditProposal{}, err
	}
	select {
	case <-ctx.Done():
		return EditProposal{}, ctx.Err()
	default:
	}
	current, err := os.ReadFile(fullPath)
	if err != nil {
		return EditProposal{}, err
	}
	if len(current) > maxProposalBytes {
		return EditProposal{}, errors.New("current file is too large")
	}
	if !utf8.Valid(current) {
		return EditProposal{}, errors.New("current file is not text")
	}
	proposal := EditProposal{
		ID:        newTraceID(),
		Path:      displayPath,
		Summary:   strings.TrimSpace(input.Summary),
		Status:    "pending",
		BaseHash:  contentHash(current),
		Content:   input.Content,
		Diff:      buildDiff(string(current), input.Content),
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.editProposals = append([]EditProposal{proposal}, r.editProposals...)
	if len(r.editProposals) > 25 {
		r.editProposals = r.editProposals[:25]
	}
	return proposal, nil
}

func (r *Runtime) ReviewEditProposal(_ context.Context, id string, input EditProposalReviewInput) (EditProposal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EditProposal{}, errors.New("Edit proposal ID is required.")
	}
	status := strings.TrimSpace(input.Status)
	switch status {
	case "approved", "rejected":
	default:
		return EditProposal{}, errors.New("Edit proposal status is invalid.")
	}
	detail := strings.TrimSpace(input.Detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	reviewedAt := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.editProposals {
		if r.editProposals[index].ID == id {
			r.editProposals[index].Status = status
			r.editProposals[index].ReviewDetail = detail
			r.editProposals[index].ReviewedAt = &reviewedAt
			return r.editProposals[index], nil
		}
	}
	return EditProposal{}, errors.New("Edit proposal was not found.")
}

func (r *Runtime) ApplyEditProposal(ctx context.Context, id string) (EditProposal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EditProposal{}, errors.New("Edit proposal ID is required.")
	}
	r.mu.RLock()
	var proposal EditProposal
	found := false
	for _, item := range r.editProposals {
		if item.ID == id {
			proposal = item
			found = true
			break
		}
	}
	r.mu.RUnlock()
	if !found {
		return EditProposal{}, errors.New("Edit proposal was not found.")
	}
	if proposal.Status != "approved" {
		return EditProposal{}, errors.New("Edit proposal must be approved before applying.")
	}
	fullPath, displayPath, err := r.workspacePath(proposal.Path)
	if err != nil {
		return EditProposal{}, err
	}
	select {
	case <-ctx.Done():
		return EditProposal{}, ctx.Err()
	default:
	}
	current, err := os.ReadFile(fullPath)
	if err != nil {
		return EditProposal{}, err
	}
	if contentHash(current) != proposal.BaseHash {
		return EditProposal{}, errors.New("Edit proposal is stale. Review the latest file before applying.")
	}
	if err := os.WriteFile(fullPath, []byte(proposal.Content), 0o600); err != nil {
		return EditProposal{}, err
	}
	appliedAt := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.editProposals {
		if r.editProposals[index].ID == id {
			r.editProposals[index].Path = displayPath
			r.editProposals[index].Status = "applied"
			r.editProposals[index].AppliedAt = &appliedAt
			return r.editProposals[index], nil
		}
	}
	return EditProposal{}, errors.New("Edit proposal was not found.")
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func buildDiff(before string, after string) []DiffLine {
	oldLines := splitDiffLines(before)
	newLines := splitDiffLines(after)
	if len(oldLines)*len(newLines) > 200000 {
		return []DiffLine{
			{Type: "remove", OldLine: 1, Text: before},
			{Type: "add", NewLine: 1, Text: after},
		}
	}
	table := make([][]int, len(oldLines)+1)
	for i := range table {
		table[i] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	diff := []DiffLine{}
	i, j := 0, 0
	for i < len(oldLines) && j < len(newLines) {
		if oldLines[i] == newLines[j] {
			diff = append(diff, DiffLine{Type: "equal", OldLine: i + 1, NewLine: j + 1, Text: oldLines[i]})
			i++
			j++
			continue
		}
		if table[i+1][j] >= table[i][j+1] {
			diff = append(diff, DiffLine{Type: "remove", OldLine: i + 1, Text: oldLines[i]})
			i++
		} else {
			diff = append(diff, DiffLine{Type: "add", NewLine: j + 1, Text: newLines[j]})
			j++
		}
	}
	for ; i < len(oldLines); i++ {
		diff = append(diff, DiffLine{Type: "remove", OldLine: i + 1, Text: oldLines[i]})
	}
	for ; j < len(newLines); j++ {
		diff = append(diff, DiffLine{Type: "add", NewLine: j + 1, Text: newLines[j]})
	}
	return diff
}

func splitDiffLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if content == "" {
		return []string{}
	}
	return strings.Split(content, "\n")
}
