package io.ycvk.acorn.core.sse

import io.ycvk.acorn.api.models.ClientRunStartedEvent

/**
 * Projects a stream of run events into UI state.
 *
 * Skeleton for B2: the concrete projection (assistant message assembly, run lifecycle,
 * pending-action surfacing) is implemented alongside the real RunEvent deserializer.
 */
class RunEventProjection {

    /**
     * Applies [event] to the projection. Returns the new accumulated state, or the
     * previous state if the event is ignored. B2 will populate the real state machine.
     */
    fun apply(state: RunEventProjectionState, event: ClientRunStartedEvent): RunEventProjectionState {
        // TODO(b2): implement run-event → UI state projection.
        return state.copy(lastSeq = event.seq)
    }
}

/**
 * Snapshot of the projected run state. B2 will expand this into the real view model.
 */
data class RunEventProjectionState(
    val lastSeq: Long = 0L,
    val terminal: Boolean = false,
)
