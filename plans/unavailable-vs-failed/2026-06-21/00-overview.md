# 00 — Overview: Distinguish "Unavailable" from "Failed"

## Problem

Currently every dead-end — restricted album, no MP3s, poor quality decode, non-transient
API error — gets lumped into `status='failed'`. The retry system and `ResetAllFailed`
clear them, creating churn on definitively dead items.

## Current Behavior

| Scenario | Status | Retry behavior |
|---|---|---|
| Access-restricted item | `failed` | `ResetAllFailed` resets to pending |
| No acceptable MP3 tracks | `failed` | `ResetAllFailed` resets to pending |
| MP3 decode failure | `failed` | `ResetAllFailed` resets to pending |
| Low quality score | `failed` | `ResetAllFailed` resets to pending |
| Non-transient metadata error | `failed` | `ResetAllFailed` resets to pending |
| Stream/network error | `failed` | `RequeueAlbumForRetry` (max 3x) |
| CLAP server error | `failed` | `RequeueTrackForRetry` |
| DB write error | `failed` | `RequeueTrackForRetry` |

All `failed` items are treated identically by retries and bulk resets, causing
permanently dead albums/tracks to be retried pointlessly.

## Design Proposal

Add a new `unavailable` status that means **"permanently dead content — retrying will
never help."** `failed` stays for **"transient error — retry may succeed."**

The distinction is applied at every call site that marks an album or track as dead.

## Classification of All Failure Call Sites

| Call site | Error condition | New status |
|---|---|---|
| Cleanup worker detects access-restricted | `FailAlbumAndPendingTracksByID` | `unavailable` |
| Download returns 401/403 | `FailAlbumAndPendingTracks` | `unavailable` |
| MP3 decode failure | `FlagAlbumPoorQuality` | `unavailable` |
| Quality score below threshold | `MarkTrackFailed` | `unavailable` |
| No acceptable MP3 tracks | `MarkAlbumFailed` | `unavailable` |
| Non-transient metadata error | `MarkAlbumFailed` | `unavailable` |
| Stream network error | `MarkTrackFailed` | `failed` (unchanged) |
| CLAP server error | `MarkTrackFailed` | `failed` (unchanged) |
| DB write error | `MarkTrackFailed` | `failed` (unchanged) |
| Transient metadata → requeue | `RequeueAlbumForRetry` | `failed` (unchanged) |
| Exhausted retries | `MarkAlbumFailed` (inside `RequeueAlbumForRetry`) | `failed` (unchanged) |
| `ResetAllFailed` | resets `failed` → `pending` | Must skip `unavailable` |

## State Machine

```
Albums:
  pending → resolving → resolved      (success)
  pending → resolving → unavailable    (permanent: no MP3s, restricted, poor quality)
  pending → resolving → failed         (transient: network, rate limit, API, DB)
  failed → pending                     (RequeueAlbumForRetry, max 3x)
  unavailable → (no automatic recovery)

Tracks:
  pending → processing → completed     (success)
  pending → processing → unavailable   (permanent: poor quality, decode fail)
  pending → processing → failed        (transient: stream, CLAP, DB write)
  failed → pending                     (RequeueTrackForRetry)
  unavailable → (no automatic recovery)
```
