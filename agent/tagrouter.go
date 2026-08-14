package main

import (
	"regexp"
	"strings"
)

var (
	// --- Behavior (backward compat, replaced by <process>) ---
	behaviorOpenRe    = regexp.MustCompile(`^\s*<behavior\b`)
	behaviorCloseRe   = regexp.MustCompile(`</behavior>`)
	desRe             = regexp.MustCompile(`(?s)<des[^>]*>(.*?)</des>`)

	// --- Process (new format, replaces <behavior><des>) ---
	processOpenRe     = regexp.MustCompile(`^\s*<process\b`)
	processCloseRe    = regexp.MustCompile(`</process>`)
	messageRe         = regexp.MustCompile(`\s*<message\b`)
	messageContentRe  = regexp.MustCompile(`(?s)<message[^>]*>(.*?)</message>`)

	// --- Report ---
	reportOpenRe      = regexp.MustCompile(`^\s*<report\b`)
	reportCloseRe     = regexp.MustCompile(`</report>`)

	// --- File action ---
	fileActionOpenRe  = regexp.MustCompile(`^\s*<file_action\b`)

	// --- Legacy response ---
	responseOpenRe    = regexp.MustCompile(`^\s*<response\b`)

	// --- Others ---
	citeRe            = regexp.MustCompile(`<cite\b[^>]*>`)
	imgMdRe           = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
)

// --- Constants for route names ---
const (
	RouteBehavior             = "behavior"
	RouteBehaviorViolation    = "behavior_violation"
	RouteBehaviorAutowrap     = "behavior_autowrap"
	RouteReport               = "report"
	RouteReportViolation      = "report_violation"
	RouteReportAutowrap       = "report_autowrap"
	RouteToolOnly             = "tool_only"
	RouteLegacyResponse       = "legacy_response"
	RouteLegacyResponseViol   = "legacy_response_violation"
	RouteEmpty                = "empty"
)

// hasOpenTag checks if a chunk opens with a specific tag
func hasOpenTag(delta string, openRe *regexp.Regexp) bool {
	return openRe.MatchString(delta)
}

// isInsideTag checks whether accumulated text is currently inside a tag pair
func isInsideTag(accumulated, openTagName, closeTag string) bool {
	if accumulated == "" {
		return false
	}
	lastOpen := strings.LastIndex(accumulated, "<"+openTagName)
	if lastOpen < 0 {
		return false
	}
	lastClose := strings.LastIndex(accumulated, closeTag)
	return lastOpen > lastClose
}

// --- Detection helpers ---

func HasBehaviorTag(content string) bool { return strings.Contains(content, "<behavior") }
func HasReportTag(content string) bool   { return strings.Contains(content, "<report") }
func HasProcessTag(content string) bool  { return strings.Contains(content, "<process") }
func HasLegacyResponseTag(content string) bool { return strings.Contains(content, "<response") }
func HasImageLinkTag(content string) bool       { return imgMdRe.MatchString(content) }
func HasCiteTag(content string) bool            { return citeRe.MatchString(content) }
func HasFileActionTag(content string) bool {
	return fileActionOpenRe.MatchString(content)
}
func HasMessageTag(content string) bool { return messageRe.MatchString(content) }

// CanStream determines if content can be streamed (has an open tag)
func CanStream(content string) bool {
	return processOpenRe.MatchString(content) ||
		behaviorOpenRe.MatchString(content) ||
		reportOpenRe.MatchString(content) ||
		fileActionOpenRe.MatchString(content) ||
		responseOpenRe.MatchString(content)
}

// ExtractFileAction extracts file_action info from content
func ExtractFileAction(content string) string {
	m := fileActionOpenRe.FindStringSubmatch(content)
	if len(m) > 0 {
		return m[0]
	}
	return ""
}

// --- Inside tag checks (for streaming) ---

func IsInsideReport(accumulated string) bool {
	return isInsideTag(accumulated, "report", "</report>")
}
func IsInsideBehavior(accumulated string) bool {
	return isInsideTag(accumulated, "behavior", "</behavior>")
}
func IsInsideProcess(accumulated string) bool {
	return isInsideTag(accumulated, "process", "</process>")
}

// GetContentRoute classifies a content chunk by route
func GetContentRoute(delta string, hasToolCalls bool, accumulatedText string) string {
	// Streaming: currently inside tags
	if IsInsideProcess(accumulatedText) {
		return RouteBehavior // process = behavior equivalent
	}
	if IsInsideBehavior(accumulatedText) {
		return RouteBehavior
	}
	if IsInsideReport(accumulatedText) {
		return RouteReport
	}

	if hasToolCalls {
		// Intermediate turn
		if hasOpenTag(delta, processOpenRe) || hasOpenTag(delta, behaviorOpenRe) {
			return RouteBehavior
		}
		if reportOpenRe.MatchString(delta) {
			return RouteReportViolation
		}
		if responseOpenRe.MatchString(delta) {
			return RouteLegacyResponseViol
		}
		if strings.TrimSpace(delta) != "" {
			return RouteBehaviorAutowrap
		}
		return RouteToolOnly
	}

	// Final turn (no tool calls)
	if reportOpenRe.MatchString(delta) {
		return RouteReport
	}
	if behaviorOpenRe.MatchString(delta) || processOpenRe.MatchString(delta) {
		return RouteBehaviorViolation
	}
	if responseOpenRe.MatchString(delta) {
		return RouteLegacyResponse
	}
	if strings.TrimSpace(delta) != "" {
		return RouteReportAutowrap
	}
	return RouteEmpty
}

// --- Description extraction (message in <process>, fallback to <des> in <behavior>) ---

// GetBehaviorDescription extracts the <des> content from a <behavior> tag (old format)
func GetBehaviorDescription(content string) string {
	if content == "" {
		return ""
	}
	m := desRe.FindStringSubmatch(content)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// GetProcessMessage extracts <message> content from a <process> tag (new format)
func GetProcessMessage(content string) string {
	if content == "" {
		return ""
	}
	m := messageContentRe.FindStringSubmatch(content)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// GetIntermediateDescription returns user-facing description for intermediate turns
// Prefers <process><message> (new format), falls back to <behavior><des> (old format)
func GetIntermediateDescription(content string) string {
	msg := GetProcessMessage(content)
	if msg != "" {
		return msg
	}
	return GetBehaviorDescription(content)
}

// ComplianceResult holds the result of a compliance check
type ComplianceResult struct {
	IsCompliant       bool
	Violations        []string
	HasReport         bool
	HasBehavior       bool
	HasProcess        bool
	HasLegacyResponse bool
	HasImageLink      bool
	HasCite           bool
	Description       string
}

// CheckFinalTurnCompliant checks if final turn content (<report> expected) is compliant
func CheckFinalTurnCompliant(finalContent string) ComplianceResult {
	hasReport := HasReportTag(finalContent)
	hasBehavior := HasBehaviorTag(finalContent)
	hasProcess := HasProcessTag(finalContent)
	hasLegacy := HasLegacyResponseTag(finalContent)
	hasImg := HasImageLinkTag(finalContent)
	hasCite := HasCiteTag(finalContent)

	violations := []string{}
	if !hasReport {
		violations = append(violations, "missing_report")
	}
	if hasBehavior {
		violations = append(violations, "behavior_in_final")
	}
	if hasProcess {
		violations = append(violations, "process_in_final")
	}
	if hasLegacy {
		violations = append(violations, "legacy_response_in_final")
	}

	reportIdx := strings.Index(finalContent, "<report")
	imgIdx := strings.Index(finalContent, "![")
	if imgIdx >= 0 && reportIdx < 0 {
		violations = append(violations, "image_link_outside_report")
	} else if imgIdx >= 0 && reportIdx >= 0 && imgIdx < reportIdx {
		violations = append(violations, "image_link_before_report")
	}
	if hasCite && reportIdx < 0 {
		violations = append(violations, "cite_outside_report")
	}

	return ComplianceResult{
		IsCompliant:       len(violations) == 0,
		Violations:        violations,
		HasReport:         hasReport,
		HasBehavior:       hasBehavior,
		HasProcess:        hasProcess,
		HasLegacyResponse: hasLegacy,
		HasImageLink:      hasImg,
		HasCite:           hasCite,
	}
}

// CheckIntermediateTurnCompliant checks if intermediate turn content is compliant
// Expected format: <process><message>...</message></process> (with optional <file_action>)
// Fallback: <behavior><des>...</des></behavior>
func CheckIntermediateTurnCompliant(content string) ComplianceResult {
	hasProcess := HasProcessTag(content)
	hasBehavior := HasBehaviorTag(content)
	hasReport := HasReportTag(content)
	hasLegacy := HasLegacyResponseTag(content)
	hasImg := HasImageLinkTag(content)
	hasCite := HasCiteTag(content)

	violations := []string{}

	// Should not have <report> in intermediate
	if hasReport {
		violations = append(violations, "report_in_intermediate")
	}
	if hasLegacy {
		violations = append(violations, "legacy_response_in_intermediate")
	}
	if hasImg {
		violations = append(violations, "image_link_in_intermediate")
	}
	if hasCite {
		violations = append(violations, "cite_in_intermediate")
	}

	// Should have <process> (new) or <behavior> (old)
	hasValidTag := hasProcess || hasBehavior
	desc := ""
	if hasProcess {
		desc = GetProcessMessage(content)
	} else if hasBehavior {
		desc = GetBehaviorDescription(content)
	}

	if !hasValidTag && strings.TrimSpace(content) != "" {
		violations = append(violations, "missing_intermediate_tag")
	} else if hasValidTag {
		if desc == "" {
			if hasProcess {
				violations = append(violations, "process_missing_message")
			}
			if hasBehavior {
				violations = append(violations, "behavior_missing_des")
			}
		}
	}

	return ComplianceResult{
		IsCompliant:       len(violations) == 0,
		Violations:        violations,
		HasReport:         hasReport,
		HasBehavior:       hasBehavior,
		HasProcess:        hasProcess,
		HasLegacyResponse: hasLegacy,
		HasImageLink:      hasImg,
		HasCite:           hasCite,
		Description:       desc,
	}
}

// WrapAsProcess wraps content in <process><message>...</message></process>
func WrapAsProcess(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || HasProcessTag(content) || HasReportTag(content) {
		return content
	}
	return "<process>\n  <message>" + content + "</message>\n</process>"
}

// WrapAsBehavior wraps content in <behavior><des>...</des></behavior> (backward compat)
func WrapAsBehavior(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || HasBehaviorTag(content) || HasReportTag(content) || HasProcessTag(content) {
		return content
	}
	return "<behavior><des>" + content + "</des></behavior>"
}

// WrapAsReport wraps content in <report>...</report> if not already wrapped
func WrapAsReport(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || HasReportTag(content) || HasBehaviorTag(content) || HasProcessTag(content) {
		return content
	}
	return "<report>\n" + content + "\n</report>"
}

// RepairIntermediateContent auto-wraps content if it lacks <process> or <behavior> tag
func RepairIntermediateContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}
	if !HasProcessTag(content) && !HasBehaviorTag(content) && !HasReportTag(content) && !HasLegacyResponseTag(content) {
		return WrapAsProcess(content) // Use new format as default
	}
	return content
}

// FinalReportRequired checks if final content needs a <report> wrapper
func FinalReportRequired(content string) bool {
	if HasReportTag(content) || HasLegacyResponseTag(content) || HasImageLinkTag(content) || HasCiteTag(content) {
		return true
	}
	return len(strings.TrimSpace(content)) > 200
}

// RepairFinalContent fixes final turn content to be compliant
func RepairFinalContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}
	// Legacy <response> → <report>
	re := regexp.MustCompile(`(?s)^\s*<response[^>]*>(.*?)</response>\s*$`)
	if m := re.FindStringSubmatch(content); len(m) > 0 {
		return "<report>\n" + strings.TrimSpace(m[1]) + "\n</report>"
	}
	// <behavior> or <process> in final → <report>
	tagRe := regexp.MustCompile(`(?s)^\s*<(?:behavior|process)[^>]*>.*?</(?:behavior|process)>\s*$`)
	if tagRe.MatchString(content) {
		visible := GetIntermediateDescription(content)
		if visible == "" {
			visibleRe := regexp.MustCompile(`(?s)</?(?:behavior|process|message|des)[^>]*>`)
			visible = strings.TrimSpace(visibleRe.ReplaceAllString(content, ""))
		}
		return WrapAsReport(visible)
	}
	if !HasReportTag(content) {
		if FinalReportRequired(content) {
			return WrapAsReport(content)
		}
		return content
	}
	// Malformed <report>
	reportRe := regexp.MustCompile(`(?s)^\s*<report[^>]*>.*</report>\s*$`)
	if !reportRe.MatchString(content) {
		visibleRe := regexp.MustCompile(`(?s)</?report[^>]*>`)
		visible := strings.TrimSpace(visibleRe.ReplaceAllString(content, ""))
		return WrapAsReport(visible)
	}
	return content
}

// GetUserVisibleText extracts user-visible text from content (strips tags)
func GetUserVisibleText(content string) string {
	// Try <process><message> first (new format)
	if HasProcessTag(content) {
		msg := GetProcessMessage(content)
		if msg != "" {
			return msg
		}
	}
	// Try <behavior><des> (old format)
	if HasBehaviorTag(content) {
		des := GetBehaviorDescription(content)
		if des != "" {
			return des
		}
	}
	// Try <report> content
	if HasReportTag(content) {
		re := regexp.MustCompile(`(?s)<report[^>]*>(.*?)</report>`)
		if m := re.FindStringSubmatch(content); len(m) > 0 {
			return strings.TrimSpace(m[1])
		}
	}
	return content
}
