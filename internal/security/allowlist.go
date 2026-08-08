package security

import (
	"fmt"
)

var defaultDeniedNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

type NamespaceGuard struct {
	allowlist map[string]struct{}
	denylist  map[string]struct{}
}

func NewNamespaceGuard(allowed, denied []string) NamespaceGuard {
	guard := NamespaceGuard{allowlist: map[string]struct{}{}, denylist: map[string]struct{}{}}
	if denied == nil {
		denied = defaultDeniedNamespaces
	}
	for _, namespace := range allowed {
		if namespace != "" {
			guard.allowlist[namespace] = struct{}{}
		}
	}
	for _, namespace := range denied {
		if namespace != "" {
			guard.denylist[namespace] = struct{}{}
		}
	}
	return guard
}

func (g NamespaceGuard) Check(namespace string) error {
	if namespace == "" {
		return nil
	}
	if _, denied := g.denylist[namespace]; denied {
		return fmt.Errorf("namespace %q is denied", namespace)
	}
	if len(g.allowlist) == 0 {
		return nil
	}
	if _, ok := g.allowlist[namespace]; !ok {
		return fmt.Errorf("namespace %q is not allowed", namespace)
	}
	return nil
}

func (g NamespaceGuard) Filter(namespaces []string) []string {
	out := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if g.Check(namespace) == nil {
			out = append(out, namespace)
		}
	}
	return out
}
