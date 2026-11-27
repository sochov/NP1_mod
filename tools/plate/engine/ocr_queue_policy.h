#pragma once

#include <algorithm>
#include <array>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <string>
#include <utility>
#include <vector>

namespace np1::plate {

struct OCRPolicyBox {
  float x = 0, y = 0, w = 0, h = 0;
};

struct OCRPolicyCandidate {
  OCRPolicyBox box;
  double quality = 0;
  size_t source_index = 0;
};

struct OCRPolicySelection {
  size_t source_index = 0;
  uint64_t track_id = 0;
  double quality = 0;
  // 3: a vehicle without a completed OCR, 2: a materially clearer view,
  // 1: a low-rate retry while one or more fields remain unresolved.
  int priority = 0;
};

struct OCRPolicyObservation {
  std::string region, classification, kana, number;
};

class OCRQueuePolicy {
 public:
  std::vector<OCRPolicySelection> select(const std::vector<OCRPolicyCandidate>& candidates,
                                         int64_t now_ms) {
    tracks_.erase(std::remove_if(tracks_.begin(), tracks_.end(), [&](const Track& track) {
      return now_ms - track.last_seen_ms > kTrackExpiryMs;
    }), tracks_.end());

    std::vector<OCRPolicySelection> selected;
    std::vector<bool> used(tracks_.size(), false);
    for (const auto& candidate : candidates) {
      int best = -1;
      double best_score = -1e9;
      for (size_t i = 0; i < tracks_.size(); ++i) {
        if (used[i]) continue;
        const double overlap = intersection_over_union(candidate.box, tracks_[i].box);
        const double distance = normalized_distance(candidate.box, tracks_[i].box);
        if (overlap < .12 && distance > .72) continue;
        const double score = overlap - .08 * distance;
        if (score > best_score) {
          best = static_cast<int>(i);
          best_score = score;
        }
      }
      if (best < 0) {
        Track track;
        track.id = next_track_id_++;
        track.box = candidate.box;
        track.last_seen_ms = now_ms;
        tracks_.push_back(std::move(track));
        used.push_back(true);
        best = static_cast<int>(tracks_.size() - 1);
      } else {
        used[best] = true;
        tracks_[best].box = candidate.box;
        tracks_[best].last_seen_ms = now_ms;
      }

      Track& track = tracks_[best];
      int priority = 0;
      if (track.completed == 0) {
        priority = 3;
      } else {
        const bool complete = track.all_fields_confirmed();
        const double relative_gain = complete ? 1.30 : 1.12;
        const double absolute_gain = complete ? 1.50 : .75;
        if (candidate.quality >= track.best_quality * relative_gain &&
            candidate.quality >= track.best_quality + absolute_gain) {
          priority = 2;
        } else if (!complete && now_ms - track.last_queued_ms >= kIncompleteRetryMs) {
          priority = 1;
        }
      }
      if (priority != 0) {
        selected.push_back({candidate.source_index, track.id, candidate.quality, priority});
      }
    }
    return selected;
  }

  void queued(const std::vector<OCRPolicySelection>& selected, int64_t now_ms) {
    for (const auto& selection : selected) {
      if (Track* track = find(selection.track_id)) track->last_queued_ms = now_ms;
    }
  }

  void observed(uint64_t track_id, const OCRPolicyObservation& observation, double quality) {
    Track* track = find(track_id);
    if (!track) return;
    ++track->completed;
    track->best_quality = std::max(track->best_quality, quality);
    track->fields[0].observe(observation.region);
    track->fields[1].observe(observation.classification);
    track->fields[2].observe(observation.kana);
    track->fields[3].observe(observation.number);
  }

  bool all_fields_confirmed(uint64_t track_id) const {
    const Track* track = find(track_id);
    return track && track->all_fields_confirmed();
  }

 private:
  static constexpr int64_t kTrackExpiryMs = 3000;
  static constexpr int64_t kIncompleteRetryMs = 850;

  struct FieldConsensus {
    std::string value;
    int support = 0;

    void observe(const std::string& next) {
      if (next.empty()) return;
      if (next == value) {
        support = std::min(support + 1, 3);
      } else if (support <= 1) {
        value = next;
        support = 1;
      } else {
        --support;
      }
    }

    bool confirmed() const { return !value.empty() && support >= 2; }
  };

  struct Track {
    uint64_t id = 0;
    OCRPolicyBox box;
    int64_t last_seen_ms = 0;
    int64_t last_queued_ms = -kIncompleteRetryMs;
    double best_quality = 0;
    uint32_t completed = 0;
    std::array<FieldConsensus, 4> fields;

    bool all_fields_confirmed() const {
      return std::all_of(fields.begin(), fields.end(), [](const auto& field) { return field.confirmed(); });
    }
  };

  static double intersection_over_union(const OCRPolicyBox& a, const OCRPolicyBox& b) {
    const double x1 = std::max(a.x, b.x), y1 = std::max(a.y, b.y);
    const double x2 = std::min(a.x + a.w, b.x + b.w), y2 = std::min(a.y + a.h, b.y + b.h);
    const double intersection = std::max(0.0, x2 - x1) * std::max(0.0, y2 - y1);
    return intersection / (a.w * a.h + b.w * b.h - intersection + 1e-6);
  }

  static double normalized_distance(const OCRPolicyBox& a, const OCRPolicyBox& b) {
    const double dx = (a.x + a.w / 2 - b.x - b.w / 2) / std::max(1.0f, std::max(a.w, b.w));
    const double dy = (a.y + a.h / 2 - b.y - b.h / 2) / std::max(1.0f, std::max(a.h, b.h));
    return std::hypot(dx, dy);
  }

  Track* find(uint64_t id) {
    const auto found = std::find_if(tracks_.begin(), tracks_.end(),
                                    [&](const Track& track) { return track.id == id; });
    return found == tracks_.end() ? nullptr : &*found;
  }

  const Track* find(uint64_t id) const {
    const auto found = std::find_if(tracks_.begin(), tracks_.end(),
                                    [&](const Track& track) { return track.id == id; });
    return found == tracks_.end() ? nullptr : &*found;
  }

  uint64_t next_track_id_ = 1;
  std::vector<Track> tracks_;
};

}  // namespace np1::plate
