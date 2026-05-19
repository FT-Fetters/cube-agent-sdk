package agent

import (
	"context"
	"fmt"
	"strings"
)

// DenyAllApproval rejects every tool call. Use it when no local tool should run;
// for selective access, prefer AllowToolsApproval as a deny-by-default allowlist.
type DenyAllApproval struct{}

func (DenyAllApproval) ApproveTool(context.Context, ApprovalRequest) (ApprovalDecision, error) {
	return ApprovalDecision{Approved: false, Reason: "approval denied by default"}, nil
}

// AllowToolsApproval returns a policy that approves only the named tools.
func AllowToolsApproval(names ...string) ApprovalPolicy {
	return toolAllowlistApproval{names: approvalNameSet(names)}
}

type toolAllowlistApproval struct {
	names map[string]struct{}
}

func (p toolAllowlistApproval) ApproveTool(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	name := approvalRequestToolName(request)
	if _, ok := p.names[name]; ok {
		return ApprovalDecision{Approved: true, Reason: fmt.Sprintf("tool %s is allowed by approval allowlist", name)}, nil
	}
	return ApprovalDecision{Approved: false, Reason: fmt.Sprintf("tool %s is not in approval allowlist", name)}, nil
}

// DenyToolsApproval returns a policy that rejects the named tools and approves the rest.
func DenyToolsApproval(names ...string) ApprovalPolicy {
	return toolDenylistApproval{names: approvalNameSet(names)}
}

type toolDenylistApproval struct {
	names map[string]struct{}
}

func (p toolDenylistApproval) ApproveTool(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	name := approvalRequestToolName(request)
	if _, ok := p.names[name]; ok {
		return ApprovalDecision{Approved: false, Reason: fmt.Sprintf("tool %s is blocked by approval denylist", name)}, nil
	}
	return ApprovalDecision{Approved: true, Reason: fmt.Sprintf("tool %s is not blocked by approval denylist", name)}, nil
}

// AllowRisksApproval returns a policy that approves only tools with listed risks.
func AllowRisksApproval(risks ...ToolRisk) ApprovalPolicy {
	allowed := make(map[ToolRisk]struct{}, len(risks))
	for _, risk := range risks {
		allowed[risk] = struct{}{}
	}
	return riskAllowlistApproval{risks: allowed}
}

type riskAllowlistApproval struct {
	risks map[ToolRisk]struct{}
}

func (p riskAllowlistApproval) ApproveTool(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	risk := request.Risk
	if _, ok := p.risks[risk]; ok {
		return ApprovalDecision{Approved: true, Reason: fmt.Sprintf("tool risk %s is allowed", approvalRiskLabel(risk))}, nil
	}
	return ApprovalDecision{Approved: false, Reason: fmt.Sprintf("tool risk %s is not in approval risk allowlist", approvalRiskLabel(risk))}, nil
}

// RequireAllApprovals composes policies with AND semantics. The first denial or
// error stops evaluation so downstream policies cannot accidentally override it.
func RequireAllApprovals(policies ...ApprovalPolicy) ApprovalPolicy {
	return requireAllApprovals{policies: append([]ApprovalPolicy(nil), policies...)}
}

type requireAllApprovals struct {
	policies []ApprovalPolicy
}

func (p requireAllApprovals) ApproveTool(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if len(p.policies) == 0 {
		return ApprovalDecision{Approved: false, Reason: "no approval policies configured"}, nil
	}

	reasons := make([]string, 0, len(p.policies))
	for _, policy := range p.policies {
		if policy == nil {
			return ApprovalDecision{Approved: false, Reason: "approval policy is nil"}, nil
		}
		decision, err := policy.ApproveTool(ctx, request)
		if err != nil {
			return decision, err
		}
		decision = normalizeApprovalDecision(decision)
		if !decision.Approved {
			return decision, nil
		}
		if decision.Reason != "" {
			reasons = append(reasons, decision.Reason)
		}
	}

	return ApprovalDecision{Approved: true, Reason: approvalJoinedReason(reasons, "allowed")}, nil
}

func approvalNameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func approvalRequestToolName(request ApprovalRequest) string {
	if request.ToolName != "" {
		return request.ToolName
	}
	return request.ToolCall.Name
}

func normalizeApprovalDecision(decision ApprovalDecision) ApprovalDecision {
	if strings.TrimSpace(decision.Reason) != "" {
		return decision
	}
	if decision.Approved {
		decision.Reason = "approved"
	} else {
		decision.Reason = "approval denied"
	}
	return decision
}

func approvalJoinedReason(reasons []string, fallback string) string {
	if len(reasons) == 0 {
		return fallback
	}
	return strings.Join(reasons, "; ")
}

func approvalRiskLabel(risk ToolRisk) string {
	if risk == "" {
		return "unspecified"
	}
	return string(risk)
}
