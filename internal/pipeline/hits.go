package pipeline

import (
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// storedHits relocates the hits of the expanded request onto the items this call stores. A hit that
// belongs to an ancestor item is dropped: the ancestor stored its own copy when it was admitted, so
// keeping it here is what made hits accumulate across a conversation.
func (state *call) storedHits(result *redact.Result) []auditevent.Hit {
	return relocateHits(state.request, state.offset, state.recorder.Call().ID, result)
}

// relocateHits is the lane-independent relocation: offline auxiliary calls pass offset zero and
// their decoded items, which carry the same ordinals a generation call stores.
func relocateHits(request *model.Request, offset int, callID string, result *redact.Result) []auditevent.Hit {
	if result == nil || len(result.Hits) == 0 {
		return nil
	}
	shown := shownRanges(result)
	hits := make([]auditevent.Hit, 0, len(result.Hits))
	for index, hit := range result.Hits {
		section, ordinal, path, keep := relocateHit(request, offset, hit.Path)
		if !keep {
			continue
		}
		shownStart, shownEnd := shown[index].start, shown[index].end
		hits = append(hits, auditevent.Hit{
			Label: hit.Label, Action: hit.Action, ValueID: hit.ValueID,
			CallID: callID, Section: string(section), Ordinal: ordinal, Path: path,
			Start: hit.Start, End: hit.End, ShownStart: shownStart, ShownEnd: shownEnd, Secret: hit.Secret,
		})
	}
	return hits
}

// relocateHit maps one redaction path onto the section, ordinal and inner path of a stored item.
func relocateHit(request *model.Request, offset int, path string) (observation.Section, int, string, bool) {
	if index, suffix, ok := itemPath(path); ok {
		if index < offset {
			return "", 0, "", false
		}
		if index >= len(request.Items) {
			return observation.SectionParams, 0, path, true
		}
		return observation.SectionRequest, request.Items[index].Ordinal, suffix, true
	}
	if path == "instructions" {
		return observation.SectionInstructions, 0, "", true
	}
	if suffix, found := strings.CutPrefix(path, "instructions."); found {
		return observation.SectionInstructions, 0, suffix, true
	}
	if index, suffix, ok := toolPath(path); ok {
		return observation.SectionTools, index, suffix, true
	}
	return observation.SectionParams, 0, path, true
}

// itemPath parses "items[3].parts[0].text" into its item index and the path inside the item.
func itemPath(path string) (int, string, bool) {
	const prefix = "items["
	if !strings.HasPrefix(path, prefix) {
		return 0, "", false
	}
	end := strings.IndexByte(path[len(prefix):], ']')
	if end < 0 {
		return 0, "", false
	}
	index, err := strconv.Atoi(path[len(prefix) : len(prefix)+end])
	if err != nil || index < 0 {
		return 0, "", false
	}
	rest := path[len(prefix)+end+1:]
	return index, strings.TrimPrefix(rest, "."), true
}

// toolPath parses "tools[2].description" or "tools.2.description" into the tool index.
func toolPath(path string) (int, string, bool) {
	if path == "tools" {
		return 0, "", false
	}
	if strings.HasPrefix(path, "tools[") {
		end := strings.IndexByte(path[len("tools["):], ']')
		if end < 0 {
			return 0, "", false
		}
		index, err := strconv.Atoi(path[len("tools[") : len("tools[")+end])
		if err != nil || index < 0 {
			return 0, "", false
		}
		return index, strings.TrimPrefix(path[len("tools[")+end+1:], "."), true
	}
	rest, found := strings.CutPrefix(path, "tools.")
	if !found {
		return 0, "", false
	}
	segment, suffix, _ := strings.Cut(rest, ".")
	index, err := strconv.Atoi(segment)
	if err != nil || index < 0 {
		return 0, "", false
	}
	return index, strings.TrimPrefix(suffix, "."), true
}

// shownRange is the shown-text range that replaced a hit, or the zero value when the hit did not
// change the text (alert and block actions).
type shownRange struct {
	start, end int
}

// shownRanges pairs every replace hit with the mark that renders its value. Marks and hits are
// appended per text in the same text order, while spans inside one text are merged before rendering:
// walking a text's hits by start position and its marks together therefore assigns a merged
// placeholder to every hit it covers. A blocked text renders no marks at all.
func shownRanges(result *redact.Result) []shownRange {
	ranges := make([]shownRange, len(result.Hits))
	markIndex := 0
	for start := 0; start < len(result.Hits); {
		path := result.Hits[start].Path
		end := start
		blocked := false
		for end < len(result.Hits) && result.Hits[end].Path == path {
			if result.Hits[end].Action == redact.ActionBlock {
				blocked = true
			}
			end++
		}
		replacements := make([]int, 0, end-start)
		for index := start; index < end; index++ {
			if result.Hits[index].Action == redact.ActionReplace {
				replacements = append(replacements, index)
			}
		}
		if blocked || len(replacements) == 0 {
			start = end
			continue
		}
		sort.SliceStable(replacements, func(left, right int) bool {
			return result.Hits[replacements[left]].Start < result.Hits[replacements[right]].Start
		})
		position := 0
		for position < len(replacements) && markIndex < len(result.Marks) {
			hit := result.Hits[replacements[position]]
			mark := result.Marks[markIndex]
			switch {
			case hit.End <= mark.Start:
				position++
			case hit.Start >= mark.End:
				markIndex++
			default:
				for position < len(replacements) && result.Hits[replacements[position]].Start < mark.End {
					ranges[replacements[position]] = shownRange{start: mark.ShownStart, end: mark.ShownEnd}
					position++
				}
				markIndex++
			}
		}
		start = end
	}
	return ranges
}

// applyRedactionSummary stores the distinct value and label counts the console shows. A value
// repeated inside the call is counted once; a value repeated in an ancestor was already counted by
// that ancestor because its hit was dropped here.
func applyRedactionSummary(record *entity.Call, hits []auditevent.Hit) {
	values := map[string]struct{}{}
	labels := map[string]struct{}{}
	for _, hit := range hits {
		if hit.ValueID != "" {
			values[hit.ValueID] = struct{}{}
		}
		if hit.Label != "" {
			labels[hit.Label] = struct{}{}
		}
	}
	record.RedactionValues = len(values)
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	record.RedactionLabels = names
}
