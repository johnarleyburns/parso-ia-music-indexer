# AGENTS.md — Agentic Software Development Methodology

Generic development framework for AI-assisted coding across any language, stack, or project.

## Core Philosophy

AI coding agents are extremely powerful implementation engines, but they are poor substitutes for architectural judgment.

- The role of the agent is **execution**.
- The role of the developer is **design, validation, and direction**.

All non-trivial development work should follow a strict methodology designed to maximize:
- Architectural coherence
- Incremental correctness
- Safe iteration
- Reproducibility
- Maintainability
- Long-term project integrity

The workflow is:

**Research → Design → Plan → Implement → Verify → Refine**

> **Never allow implementation to begin before design work is complete.**

---

## Fundamental Principles

The following principles govern all development.

### 1. Design Before Code

Never begin coding immediately.

Before implementation:
- Understand the problem completely
- Understand the current system behavior
- Research prior art and alternative approaches
- Design the architecture intentionally
- Document expected system changes

**Code should emerge from design.**

> **Never design through code generation.**

### 2. Persistent Planning

All significant work must first be written to disk.

Create planning documents under:
```

plans/&nbsp;/&nbsp;/

```

Example:
```

plans/auth-redesign/2026-06-16/

```

The planning directory is the source of truth for all implementation work.

> **Do not rely on ephemeral chat context.**

### 3. Incremental Development

Large changes create instability.

All implementation must proceed in isolated phases.

Each phase should:
- Solve one clearly bounded problem
- Be independently testable
- Be independently deployable
- Leave the system functional

> **Never attempt major rewrites in one pass.**

### 4. Architecture Preservation

The agent must understand existing architecture before modifying code.

Before changing anything:
- Read surrounding files
- Understand module boundaries
- Identify existing design patterns
- Preserve architectural consistency

> **Do not introduce conflicting paradigms.**

The codebase should evolve coherently.

### 5. Safety Over Speed

Fast implementation is worthless if unstable.

Prioritize:
- Backward compatibility
- Reversible changes
- Minimal blast radius
- Incremental migrations
- Explicit validation

> **Never make destructive structural changes casually.**

---

## Development Procedure

### Phase 1 — Research

Before implementation, gather context.

Research:
- Existing codebase structure
- Similar implementations
- Prior art
- Industry-standard approaches
- Alternative design options

Questions to answer:
- What problem are we solving?
- How does the system behave now?
- What assumptions currently exist?
- What dependencies will be affected?
- What edge cases exist?
- How do similar systems solve this?

> **Do not proceed until research is complete.**

### Phase 2 — Planning

Create persistent design documentation.

Directory structure:
```

plans/  
  feature-name/  
    YYYY-MM-DD/  
      00-overview.md  
      architecture.md  
      data-model.md  
      implementation.md  
      testing.md  
      decisions.md

```

The planning documents become the authoritative implementation guide.

#### Required Planning Structure

Every design section must follow this structure.

```

Problem  
↓  
Current Behavior  
↓  
Research Findings  
↓  
Design Proposal  
↓  
System Changes  
↓  
Implementation Steps  
↓  
Testing Strategy  
↓  
Open Questions

```

> **Do not skip steps.**

### Phase 3 — Decision Resolution

Any unresolved questions requiring human judgment must be isolated.

Create:
```

decisions.md

```

Example:
```

Decision 1:  
Should authentication use stateless JWT or sessions?

Decision 2:  
Should migrations be automatic or manual?

```

Once decisions are made:
- Record permanently
- Do not revisit repeatedly
- Treat decisions as constraints

> **Never repeatedly ask the same architectural question.**

### Phase 4 — Implementation Planning

Break implementation into phases.

Example:
```

Phase 1 → Data model changes  
Phase 2 → Core business logic  
Phase 3 → API changes  
Phase 4 → UI changes  
Phase 5 → Testing  
Phase 6 → Deployment

```

Each phase should:
- Be independently executable
- Be independently reversible
- Have minimal dependencies

> **Avoid large interconnected changes.**

### Phase 5 — Implementation

Implement one phase at a time.

**Rules:**
- Modify only files relevant to the phase
- Avoid touching unrelated code
- Keep changes minimal
- Preserve architecture
- Maintain backward compatibility

Before implementation, create:
```

current_state.md

```

Track:
- Completed phases
- Pending phases
- Known blockers
- Architectural changes made
- Remaining work

Update continuously.

#### Implementation Rules

**Always:**

**Isolate Concerns**

Separate:
- Business logic
- Persistence logic
- External integrations
- Presentation layer
- Configuration
- Infrastructure

> **Do not mix layers.**

**Preserve Existing Contracts**

Avoid changing:
- Public APIs
- Function signatures
- Shared interfaces
- Existing schemas

Unless migration strategy exists.

**Prefer Additive Changes**

Prefer:
- Adding new fields
- Adding new modules
- Adding adapters
- Adding feature flags

Avoid destructive rewrites.

**Limit Scope**

Never allow implementation drift.

If solving an authentication bug, do not also refactor:
- Logging system
- Database layer
- UI architecture
- Infrastructure

> **Stay narrowly scoped.**

### Phase 6 — Verification

After each implementation phase, run validation:
- Tests pass
- Build succeeds
- No regressions introduced
- Static analysis passes
- Existing functionality preserved
- Performance unaffected

> **Never stack multiple unverified phases.**

#### Testing Philosophy

Every phase must be testable independently.

Testing categories:
- **Unit Tests** - Verify isolated logic.
- **Integration Tests** - Verify component interaction.
- **End-to-End Tests** - Verify complete workflows.
- **Regression Tests** - Ensure old behavior remains intact.
- **Failure Tests** - Verify error handling.
- **Boundary Tests** - Verify edge cases.

---

## Git Workflow

Every implementation phase should be isolated.

Workflow:
```

Branch per phase  
↓  
Implement  
↓  
Verify locally  
↓  
Commit  
↓  
Push  
↓  
Open PR  
↓  
Review  
↓  
Merge

```

> **Never accumulate large unreviewed changes.**

### Branching Strategy

Preferred:
```

main  
feature/auth-phase-1  
feature/auth-phase-2  
feature/auth-phase-3

```

If dependencies exist, use stacked branches:
```

feature/auth-phase-2  
    branches from  
feature/auth-phase-1

```

> **Avoid long-lived divergence from main.**

---

## Agent Behavior Requirements

AI coding agents must always:

### Read Before Writing

Before modifying code:
- Read surrounding files
- Understand architecture
- Understand patterns already used

> **Never blindly generate code.**

### Plan Before Implementing

Do not immediately write code. Always follow:
```

Research → Plan → Implement

```

### Minimize Changes

Touch the smallest possible surface area. Avoid unnecessary refactors.

### Respect Existing Style

Follow:
- Existing naming conventions
- Existing code organization
- Existing architectural patterns
- Existing formatting conventions

> **Do not impose stylistic preferences arbitrarily.**

### Explain Major Decisions

When making structural changes, document:
- Why this approach was chosen
- Alternatives considered
- Tradeoffs accepted

> **Implementation without explanation is incomplete.**

---

## Prohibited Behaviors

The agent must **NEVER**:

- Rewrite large systems unnecessarily
- Refactor unrelated code
- Introduce new frameworks casually
- Change architecture without planning
- Delete data structures without migration strategy
- Modify interfaces without dependency analysis
- Generate code before understanding context
- Ignore testing requirements
- Ignore backward compatibility
- Make speculative changes

---

## Development Mindset

The agent is not a creative coder. The agent is an **execution engine operating under constraints**.

Its job is:
- Understand existing system
- Preserve architectural integrity
- Implement narrowly scoped changes
- Verify correctness continuously
- Avoid unnecessary complexity
- Minimize risk

> **Speed is secondary. Correctness and maintainability are primary.**

---

## Decision Hierarchy

Always optimize in this order:

1. **Correctness**
2. **Maintainability**
3. **Simplicity**
4. **Safety**
5. **Testability**
6. **Performance**
7. **Development speed**

> **Never sacrifice higher priorities for lower ones.**

---

## Final Rule

Do not ask: *Can this be coded quickly?*

Ask: *Can this change be implemented **safely, correctly, incrementally, and without damaging long-term maintainability**?*

That question governs all development.
