#include <cassert>
#include <vector>

#include "../tools/plate/engine/ocr_queue_policy.h"

using np1::plate::OCRPolicyBox;
using np1::plate::OCRPolicyCandidate;
using np1::plate::OCRPolicyObservation;
using np1::plate::OCRQueuePolicy;

int main() {
  OCRQueuePolicy policy;
  const OCRPolicyBox first{100, 100, 80, 40};
  auto selected = policy.select({OCRPolicyCandidate{first, 10, 0}}, 0);
  assert(selected.size() == 1 && selected[0].priority == 3);
  const auto id = selected[0].track_id;
  policy.queued(selected, 0);

  // A pending/new vehicle is still marked high-priority. The bounded queue
  // performs the pending-similarity suppression; a dropped job can retry.
  selected = policy.select({OCRPolicyCandidate{{102, 101, 80, 40}, 10.2, 0}}, 100);
  assert(selected.size() == 1 && selected[0].track_id == id && selected[0].priority == 3);

  const OCRPolicyObservation complete{"横浜", "505", "ら", "7119"};
  policy.observed(id, complete, 10.2);
  policy.observed(id, complete, 10.4);
  assert(policy.all_fields_confirmed(id));

  // Once every field is confirmed, neither time nor a modest quality gain is
  // enough. A clearly better crop is admitted.
  selected = policy.select({OCRPolicyCandidate{{104, 102, 82, 41}, 12.9, 0}}, 2000);
  assert(selected.empty());
  selected = policy.select({OCRPolicyCandidate{{105, 102, 84, 42}, 14.0, 0}}, 2100);
  assert(selected.size() == 1 && selected[0].priority == 2);

  // An unresolved vehicle is retried, but only at the low-rate interval.
  OCRQueuePolicy incomplete;
  selected = incomplete.select({OCRPolicyCandidate{first, 10, 0}}, 0);
  const auto incomplete_id = selected[0].track_id;
  incomplete.queued(selected, 0);
  incomplete.observed(incomplete_id, OCRPolicyObservation{"", "", "", "7119"}, 10);
  assert(incomplete.select({OCRPolicyCandidate{{101, 100, 80, 40}, 10.1, 0}}, 500).empty());
  selected = incomplete.select({OCRPolicyCandidate{{102, 100, 80, 40}, 10.1, 0}}, 900);
  assert(selected.size() == 1 && selected[0].priority == 1);

  // Two simultaneous plates receive independent identities.
  OCRQueuePolicy multiple;
  selected = multiple.select({{{100, 100, 80, 40}, 10, 0}, {{500, 100, 80, 40}, 9, 1}}, 0);
  assert(selected.size() == 2 && selected[0].track_id != selected[1].track_id);
}
