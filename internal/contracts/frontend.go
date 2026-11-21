package contracts

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrFrontendBindingUnavailable = errors.New("frontend client has no verified handler")
	ErrFrontendBindingAmbiguous   = errors.New("frontend client has multiple verified handlers")
)

// FrontendClient is one compiler-proven call and its owner-approved target.
// Target includes the service and namespace binding supplied by configuration.
type FrontendClient struct {
	Caller    model.SymbolRef
	Site      model.SourceRef
	ServiceID *string
	Target    Key
}

// FrontendHandler is a backend adapter's explicit operation to symbol binding.
type FrontendHandler struct {
	Operation   Entity
	Handler     model.SymbolRef
	Declaration model.SourceRef
}

// FrontendBinding retains both typed endpoints and their source proof.
type FrontendBinding struct {
	Client  FrontendClient
	Handler FrontendHandler
}

// MatchFrontendHandler resolves a static call within an already authorized
// scope. The callback binds each source to its immutable bytes and current
// authorization. Observe rechecks the current authority of the pinned scope.
func MatchFrontendHandler(ctx context.Context, pinned model.ScopeVector, client FrontendClient, handlers []FrontendHandler,
	checkSource func(context.Context, model.ScopeMember, model.SourceRef) error,
	observe func(context.Context, model.ScopeVector) (model.ScopeVector, error)) (FrontendBinding, error) {
	if ctx == nil || checkSource == nil || observe == nil || len(handlers) > 4096 || client.Target.Protocol != "http" ||
		client.Target.ServiceID == nil || *client.Target.ServiceID == "" || client.Target.Environment == "" ||
		!namespacePattern.MatchString(client.Target.Namespace) || client.Target.Identifier == "" || client.Target.Version == "" ||
		client.Caller.GraphIdentity != identity(client.Site) {
		return FrontendBinding{}, ErrDomainRecord
	}
	if err := model.VerifyScopeVectorDigest(pinned, maxBatchBytes); err != nil {
		return FrontendBinding{}, err
	}
	bindSource := func(ref model.GraphRef, source model.SourceRef, service *string) error {
		member, err := model.MatchPinnedMember(pinned, ref, service)
		if err != nil {
			return err
		}
		sourceMember, err := model.MatchPinnedSource(pinned, source, service, maxBatchBytes)
		if err != nil {
			return err
		}
		if member.WorktreeID != sourceMember.WorktreeID || member.ProfileID != sourceMember.ProfileID || member.GenerationID != sourceMember.GenerationID {
			return ErrDomainRecord
		}
		return checkSource(ctx, member, source)
	}
	if err := bindSource(&client.Caller, client.Site, client.ServiceID); err != nil {
		return FrontendBinding{}, err
	}
	var selected *FrontendHandler
	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			return FrontendBinding{}, err
		}
		key := handler.Operation.Key
		if key.ServiceID == nil || *key.ServiceID != *client.Target.ServiceID || key.Environment != client.Target.Environment ||
			key.Protocol != client.Target.Protocol || key.Namespace != client.Target.Namespace ||
			key.Identifier != client.Target.Identifier || key.Version != client.Target.Version {
			continue
		}
		if handler.Handler.GraphIdentity != handler.Operation.Ref.GraphIdentity ||
			identity(handler.Declaration) != handler.Operation.Ref.GraphIdentity ||
			identity(handler.Operation.Declaration) != handler.Operation.Ref.GraphIdentity {
			return FrontendBinding{}, ErrDomainRecord
		}
		if err := bindSource(&handler.Operation.Ref, handler.Operation.Declaration, key.ServiceID); err != nil {
			return FrontendBinding{}, err
		}
		if err := bindSource(&handler.Handler, handler.Declaration, key.ServiceID); err != nil {
			return FrontendBinding{}, err
		}
		if selected != nil {
			return FrontendBinding{}, ErrFrontendBindingAmbiguous
		}
		selected = &handler
	}
	if selected == nil {
		return FrontendBinding{}, ErrFrontendBindingUnavailable
	}
	observed, err := observe(ctx, pinned)
	if err != nil {
		return FrontendBinding{}, err
	}
	if err := model.ComparePinnedScope(pinned, observed, maxBatchBytes); err != nil {
		return FrontendBinding{}, err
	}
	if err := ctx.Err(); err != nil {
		return FrontendBinding{}, err
	}
	return FrontendBinding{Client: client, Handler: *selected}, nil
}
