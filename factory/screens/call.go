package screens

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// handleCall is POST /api/call/{name}: an exhaustive switch over every call
// name a screen makes, decoding the request body into that call's own
// argument struct and invoking the one Calls method that performs it. A
// dispatch keyed by a map of strings to functions is the dispatch the code
// rules refuse; this switch is what a static reader enumerates in its
// place, and its default case is the one place a call name resolves to
// nothing.
func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := principalFrom(ctx)
	switch r.PathValue("name") {
	case "supplyIntent":
		var args SupplyIntentArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.SupplyIntent(ctx, p, args)
		writeCallResult(w, id, err)
	case "answerQuestion":
		var args AnswerQuestionArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.AnswerQuestion(ctx, p, args))
	case "confirmReading":
		var args ConfirmReadingArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.ConfirmReading(ctx, p, args))
	case "acceptDelivery":
		var args AcceptDeliveryArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.AcceptDelivery(ctx, p, args))
	case "endIntent":
		var args EndIntentArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EndIntent(ctx, p, args))
	case "setPriority":
		var args SetPriorityArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.SetPriority(ctx, p, args))
	case "endItem":
		var args EndItemArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EndItem(ctx, p, args))
	case "decide":
		var args DecideArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.Decide(ctx, p, args))
	case "approveThroughHold":
		var args ApproveThroughHoldArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.ApproveThroughHold(ctx, p, args))
	case "refer":
		var args ReferArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.Refer(ctx, p, args))
	case "editInPlace":
		var args EditInPlaceArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EditInPlace(ctx, p, args))
	case "acknowledge":
		var args AcknowledgeArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.Acknowledge(ctx, p, args))
	case "takeOver":
		var args TakeOverArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.TakeOver(ctx, p, args))
	case "supplyIntentConstraint":
		var args SupplyIntentConstraintArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.SupplyIntentConstraint(ctx, p, args)
		writeCallResult(w, id, err)
	case "withdrawConstraint":
		var args WithdrawConstraintArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawConstraint(ctx, p, args))
	case "clearCeiling":
		var args ClearCeilingArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.ClearCeiling(ctx, p, args))
	case "rollBack":
		var args RollBackArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.RollBack(ctx, p, args))
	case "raiseRevert":
		var args RaiseRevertArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.RaiseRevert(ctx, p, args))
	case "startMitigation":
		var args StartMitigationArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.StartMitigation(ctx, p, args)
		writeCallResult(w, id, err)
	case "endMitigation":
		var args EndMitigationArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EndMitigation(ctx, p, args))
	case "markRollbackNotCaused":
		var args MarkRollbackNotCausedArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.MarkRollbackNotCaused(ctx, p, args))
	case "firePage":
		var args FirePageArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.FirePage(ctx, p, args))
	case "createProject":
		var args CreateProjectArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.CreateProject(ctx, p, args)
		writeCallResult(w, id, err)
	case "declareArea":
		var args DeclareAreaArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.DeclareArea(ctx, p, args)
		writeCallResult(w, id, err)
	case "authorParameter":
		var args AuthorParameterArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.AuthorParameter(ctx, p, args))
	case "placeSafeguard":
		var args PlaceSafeguardArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.PlaceSafeguard(ctx, p, args)
		writeCallResult(w, id, err)
	case "withdrawSafeguard":
		var args WithdrawSafeguardArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawSafeguard(ctx, p, args))
	case "setHalt":
		var args SetHaltArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.SetHalt(ctx, p, args)
		writeCallResult(w, id, err)
	case "withdrawHalt":
		var args WithdrawHaltArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawHalt(ctx, p, args))
	case "setLegalHold":
		var args SetLegalHoldArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.SetLegalHold(ctx, p, args)
		writeCallResult(w, id, err)
	case "withdrawLegalHold":
		var args WithdrawLegalHoldArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawLegalHold(ctx, p, args))
	case "writeFleetEntry":
		var args WriteFleetEntryArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.WriteFleetEntry(ctx, p, args)
		writeCallResult(w, id, err)
	case "withdrawFleetEntry":
		var args WithdrawFleetEntryArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawFleetEntry(ctx, p, args))
	case "supplyConstraint":
		var args SupplyConstraintArgs
		if !decodeBody(w, r, &args) {
			return
		}
		id, err := s.calls.SupplyConstraint(ctx, p, args)
		writeCallResult(w, id, err)
	case "setSeam5Enforced":
		var args SetSeam5EnforcedArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.SetSeam5Enforced(ctx, p, args))
	case "retireService":
		var args RetireServiceArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.RetireService(ctx, p, args))
	case "endProject":
		var args EndProjectArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EndProject(ctx, p, args))
	case "decideRecordRow":
		var args DecideRecordRowArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.DecideRecordRow(ctx, p, args))
	case "editRecordRow":
		var args EditRecordRowArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.EditRecordRow(ctx, p, args))
	case "declareDuty":
		var args DeclareDutyArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.DeclareDuty(ctx, p, args))
	case "withdrawDuty":
		var args WithdrawDutyArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawDuty(ctx, p, args))
	case "declareObligation":
		var args DeclareObligationArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.DeclareObligation(ctx, p, args))
	case "withdrawObligation":
		var args WithdrawObligationArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WithdrawObligation(ctx, p, args))
	case "lendCredential":
		var args LendCredentialArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.LendCredential(ctx, p, args))
	case "takeBackCredential":
		var args TakeBackCredentialArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.TakeBackCredential(ctx, p, args))
	case "authorCeiling":
		var args AuthorCeilingArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.AuthorCeiling(ctx, p, args))
	case "authorRate":
		var args AuthorRateArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.AuthorRate(ctx, p, args))
	case "writeMapping":
		var args WriteMappingArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.WriteMapping(ctx, p, args))
	case "deleteMapping":
		var args DeleteMappingArgs
		if !decodeBody(w, r, &args) {
			return
		}
		writeCallError(w, s.calls.DeleteMapping(ctx, p, args))
	default:
		// A call name is not an address: the switch above is every name this
		// server performs, the client composes each of them from a method of
		// [Calls], and one that resolves to nothing is a client built against
		// a different shape. 404 would render as a record that is not there.
		writeError(w, http.StatusBadRequest, "screens: no call named "+r.PathValue("name"))
	}
}

// bodyLimit is the largest call body this server reads. The largest thing a
// call carries is a document a human typed at a gate — a spec version, an
// implementation plan — and a mebibyte is far above any of them, so the limit
// is not a bound on what a human may author but a bound on what an unread
// stream may cost.
const bodyLimit = 1 << 20

// bodyDeadline is how long the body of one call may take to arrive. It is set
// per request rather than as the server's ReadTimeout because the same server
// holds a server-sent-events connection open for as long as a screen is, and a
// read deadline on the connection would end it.
const bodyDeadline = 30 * time.Second

// decodeBody decodes the request body into v, writing the 400 the design
// gives a bad body and reporting false where it could not.
//
// A body over [bodyLimit] is refused with 413 and a body that does not arrive
// inside [bodyDeadline] is refused as a bad body, so neither an unbounded
// stream nor a stalled one holds a handler. An unknown field is refused too:
// every call's arguments are one struct, the client composes each object from
// that struct's own fields, and a field this server does not know is a client
// built against a different shape — the same defect the version refusal
// catches, met where the version happens to match.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	// The error is discarded: not every ResponseWriter reaches a connection —
	// a recorded response in a test does not — and one that cannot take a
	// deadline is not a reason to refuse the call.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyDeadline))
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "screens: the call body is over the limit this server reads")
			return false
		}
		writeError(w, http.StatusBadRequest, "screens: "+err.Error())
		return false
	}
	return true
}

// writeCallError answers a call that returns only an error: no content on
// success, and the error's own status otherwise.
func writeCallError(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeCallResult answers a call that creates an address: its id as JSON on
// success, and the error's own status otherwise.
func writeCallResult(w http.ResponseWriter, id string, err error) {
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, map[string]string{"id": id})
}
