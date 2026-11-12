package model

import "fmt"

// Request is the closed set of registry request values. Decoding does not
// dispatch, authorize, access storage or execute an operation.
type Request interface{ requestValue() }

func (StatusRequest) requestValue()                 {}
func (LocateRequest) requestValue()                 {}
func (PackRequest) requestValue()                   {}
func (ExpandRequest) requestValue()                 {}
func (ValidateRequest) requestValue()               {}
func (GraphQueryRequest) requestValue()             {}
func (SourceReadRequest) requestValue()             {}
func (MappingResolveRequest) requestValue()         {}
func (FlowQueryRequest) requestValue()              {}
func (BootstrapRequest) requestValue()              {}
func (PreviewAttachRequest) requestValue()          {}
func (PreviewBindRequest) requestValue()            {}
func (PreviewModeRequest) requestValue()            {}
func (PreviewAckRequest) requestValue()             {}
func (PreviewInvalidateRequest) requestValue()      {}
func (PreviewDetachRequest) requestValue()          {}
func (MappingProposeRequest) requestValue()         {}
func (MappingReviewRequest) requestValue()          {}
func (PreviewStatusRequest) requestValue()          {}
func (PreviewPermissionAdminRequest) requestValue() {}
func (GroupAdminRequest) requestValue()             {}
func (RuntimeConfigRequest) requestValue()          {}
func (RecordListRequest) requestValue()             {}
func (RecordReadRequest) requestValue()             {}
func (ProfileAdminRequest) requestValue()           {}
func (PolicyAdminRequest) requestValue()            {}
func (*RenameInitialRequest) requestValue()         {}
func (*RenameContinueRequest) requestValue()        {}
func requestResult[T Request](v T, err error) (Request, error) {
	if err != nil {
		return nil, err
	}
	return v, nil
}

func DecodeRequestJSON(schema string, data []byte, maxBytes, cpus int) (Request, error) {
	switch schema {
	case "StatusRequest":
		return requestResult(DecodeStatusRequestJSON(data, maxBytes))
	case "LocateRequest":
		return requestResult(DecodeLocateRequestJSON(data, maxBytes))
	case "PackRequest":
		return requestResult(DecodePackRequestJSON(data, maxBytes))
	case "ExpandRequest":
		return requestResult(DecodeExpandRequestJSON(data, maxBytes))
	case "ValidateRequest":
		return requestResult(DecodeValidateRequestJSON(data, maxBytes))
	case "GraphQueryRequest":
		return requestResult(DecodeGraphQueryRequestJSON(data, maxBytes))
	case "SourceReadRequest":
		return requestResult(DecodeSourceReadRequestJSON(data, maxBytes))
	case "MappingResolveRequest":
		return requestResult(DecodeMappingResolveRequestJSON(data, maxBytes))
	case "FlowQueryRequest":
		return requestResult(DecodeFlowQueryRequestJSON(data, maxBytes))
	case "BootstrapRequest":
		return requestResult(DecodeBootstrapRequestJSON(data, maxBytes))
	case "PreviewAttachRequest":
		return requestResult(DecodePreviewAttachRequestJSON(data, maxBytes))
	case "PreviewBindRequest":
		return requestResult(DecodePreviewBindRequestJSON(data, maxBytes))
	case "PreviewModeRequest":
		return requestResult(DecodePreviewModeRequestJSON(data, maxBytes))
	case "PreviewAckRequest":
		return requestResult(DecodePreviewAckRequestJSON(data, maxBytes))
	case "PreviewInvalidateRequest":
		return requestResult(DecodePreviewInvalidateRequestJSON(data, maxBytes))
	case "PreviewDetachRequest":
		return requestResult(DecodePreviewDetachRequestJSON(data, maxBytes))
	case "MappingProposeRequest":
		return requestResult(DecodeMappingProposeRequestJSON(data, maxBytes))
	case "MappingReviewRequest":
		return requestResult(DecodeMappingReviewRequestJSON(data, maxBytes))
	case "PreviewStatusRequest":
		return requestResult(DecodePreviewStatusRequestJSON(data, maxBytes))
	case "PreviewPermissionAdminRequest":
		return requestResult(DecodePreviewPermissionAdminRequestJSON(data, maxBytes))
	case "GroupAdminRequest":
		return requestResult(DecodeGroupAdminRequestJSON(data, maxBytes))
	case "RuntimeConfigRequest":
		return requestResult(DecodeRuntimeConfigRequestJSON(data, maxBytes, cpus))
	case "RecordListRequest":
		return requestResult(DecodeRecordListRequestJSON(data, maxBytes))
	case "RecordReadRequest":
		return requestResult(DecodeRecordReadRequestJSON(data, maxBytes))
	case "ProfileAdminRequest":
		return requestResult(DecodeProfileAdminRequestJSON(data, maxBytes))
	case "PolicyAdminRequest":
		return requestResult(DecodePolicyAdminRequestJSON(data, maxBytes))
	case "RenameRequest":
		v, err := DecodeRenameRequestJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		switch r := v.(type) {
		case *RenameInitialRequest:
			return r, nil
		case *RenameContinueRequest:
			return r, nil
		}
		return nil, fmt.Errorf("unknown rename variant")
	default:
		return nil, fmt.Errorf("unknown request schema")
	}
}
