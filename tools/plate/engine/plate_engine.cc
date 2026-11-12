// NP1 plate-recognition inference helper.
//
// Protocol (see backend/internal/platesensor/engine.go):
//   stdin : "NPF1" | u32 width | u32 height | u32 format(1=NV12) | u64 seq | pixels
//   stdout: one JSON line per frame. The first line is the ready line.
// Diagnostics go to stderr only; stdout carries protocol lines exclusively.
//
// Stage 1: plate detector (YOLO-style, NHWC RGB input, output [1,4+C,N] or [1,N,4+C]).
// Stage 2: plate reader, a multi-head classifier over the fixed Japanese plate
//          layout (region / class digits / kana / serial digits).
#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

#include <nlohmann/json.hpp>

#include "tensorflow/lite/interpreter.h"
#include "tensorflow/lite/kernels/register.h"
#include "tensorflow/lite/model.h"

using json = nlohmann::json;

namespace {

struct Box {
  float x, y, w, h, score;
};

struct Head {
  std::string field;
  std::vector<std::string> labels;
};

struct Config {
  float score = 0.4f, iou = 0.5f, margin = 0.06f, field_threshold = 0.6f;
  int max_plates = 4, min_width = 24;
  int roi[4] = {0, 0, 1920, 1080};
  std::vector<Head> heads;
};

uint8_t clamp8(int v) { return static_cast<uint8_t>(v < 0 ? 0 : (v > 255 ? 255 : v)); }

// Video-range BT.601 NV12 sample -> full-range RGB.
inline void nv12_rgb(const uint8_t* f, int W, int H, int x, int y, uint8_t* rgb) {
  x = std::clamp(x, 0, W - 1);
  y = std::clamp(y, 0, H - 1);
  const uint8_t* uv = f + W * H + (y / 2) * W + (x & ~1);
  int c = f[y * W + x] - 16, d = uv[0] - 128, e = uv[1] - 128;
  rgb[0] = clamp8((298 * c + 409 * e + 128) >> 8);
  rgb[1] = clamp8((298 * c - 100 * d - 208 * e + 128) >> 8);
  rgb[2] = clamp8((298 * c + 516 * d + 128) >> 8);
}

inline uint8_t nv12_gray(const uint8_t* f, int W, int H, float fx, float fy) {
  // Bilinear on luma, expanded to full range.
  fx = std::clamp(fx, 0.f, W - 1.001f);
  fy = std::clamp(fy, 0.f, H - 1.001f);
  int x0 = static_cast<int>(fx), y0 = static_cast<int>(fy);
  float ax = fx - x0, ay = fy - y0;
  const uint8_t* p = f + y0 * W + x0;
  float v = (p[0] * (1 - ax) + p[1] * ax) * (1 - ay) + (p[W] * (1 - ax) + p[W + 1] * ax) * ay;
  return clamp8(static_cast<int>(std::lround((v - 16.f) * 255.f / 219.f)));
}

// Writes a normalized [0,1] value into an input tensor of any supported type.
struct InputWriter {
  TfLiteTensor* t;
  void set(size_t i, float v) const {
    switch (t->type) {
      case kTfLiteFloat32: t->data.f[i] = v; break;
      case kTfLiteUInt8: {
        auto q = t->params;
        t->data.uint8[i] = q.scale > 0 ? clamp8(static_cast<int>(std::lround(v / q.scale + q.zero_point))) : clamp8(static_cast<int>(v * 255));
        break;
      }
      case kTfLiteInt8: {
        auto q = t->params;
        int x = static_cast<int>(std::lround(v / q.scale + q.zero_point));
        t->data.int8[i] = static_cast<int8_t>(std::clamp(x, -128, 127));
        break;
      }
      default: throw std::runtime_error("unsupported input tensor type");
    }
  }
};

float read_out(const TfLiteTensor* t, size_t i) {
  switch (t->type) {
    case kTfLiteFloat32: return t->data.f[i];
    case kTfLiteUInt8: return (t->data.uint8[i] - t->params.zero_point) * t->params.scale;
    case kTfLiteInt8: return (t->data.int8[i] - t->params.zero_point) * t->params.scale;
    default: throw std::runtime_error("unsupported output tensor type");
  }
}

std::unique_ptr<tflite::Interpreter> load(const std::string& path, int threads,
                                          std::unique_ptr<tflite::FlatBufferModel>& model) {
  model = tflite::FlatBufferModel::BuildFromFile(path.c_str());
  if (!model) throw std::runtime_error("cannot load model " + path);
  tflite::ops::builtin::BuiltinOpResolver resolver;
  std::unique_ptr<tflite::Interpreter> interp;
  tflite::InterpreterBuilder builder(*model, resolver);
  builder.SetNumThreads(threads);
  if (builder(&interp) != kTfLiteOk || !interp) throw std::runtime_error("cannot build interpreter for " + path);
  if (interp->AllocateTensors() != kTfLiteOk) throw std::runtime_error("cannot allocate tensors for " + path);
  return interp;
}

float iou(const Box& a, const Box& b) {
  float x1 = std::max(a.x, b.x), y1 = std::max(a.y, b.y);
  float x2 = std::min(a.x + a.w, b.x + b.w), y2 = std::min(a.y + a.h, b.y + b.h);
  float inter = std::max(0.f, x2 - x1) * std::max(0.f, y2 - y1);
  return inter / (a.w * a.h + b.w * b.h - inter + 1e-6f);
}

class Engine {
 public:
  Engine(const std::string& det, const std::string& ocr, const Config& cfg, int threads) : cfg_(cfg) {
    det_ = load(det, threads, det_model_);
    ocr_ = load(ocr, threads, ocr_model_);
    auto* in = det_->tensor(det_->inputs()[0]);
    if (in->dims->size != 4 || in->dims->data[3] != 3) throw std::runtime_error("detector input must be NHWC RGB");
    dh_ = in->dims->data[1];
    dw_ = in->dims->data[2];
    auto* oin = ocr_->tensor(ocr_->inputs()[0]);
    if (oin->dims->size != 4) throw std::runtime_error("reader input must be NHWC");
    oh_ = oin->dims->data[1];
    ow_ = oin->dims->data[2];
    oc_ = oin->dims->data[3];
    if (oc_ != 1 && oc_ != 3) throw std::runtime_error("reader input must have 1 or 3 channels");
    size_t total = 0;
    for (auto& h : cfg_.heads) total += h.labels.size();
    auto* oout = ocr_->tensor(ocr_->outputs()[0]);
    size_t n = 1;
    for (int i = 0; i < oout->dims->size; ++i) n *= oout->dims->data[i];
    if (n != total) throw std::runtime_error("reader output size does not match configured heads");
  }

  // Times each stage on a synthetic gray frame (device-side measurement).
  json bench(int n) {
    std::vector<uint8_t> frame(1920 * 1080 * 3 / 2, 128);
    auto time_ms = [&](auto&& fn) {
      fn();  // warm-up
      auto t0 = std::chrono::steady_clock::now();
      for (int i = 0; i < n; ++i) fn();
      return std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count() / n;
    };
    Box b{800, 600, 200, 100, 1};
    double det = time_ms([&] { detect(frame.data(), 1920, 1080); });
    double ocr = time_ms([&] { read(frame.data(), 1920, 1080, b); });
    return {{"detectorMs", det}, {"readerMs", ocr}, {"runs", n}};
  }

  json process(const uint8_t* f, int W, int H) {
    json plates = json::array();
    for (const Box& b : detect(f, W, H)) {
      if (b.w < cfg_.min_width) continue;
      json reading = read(f, W, H, b);
      plates.push_back({{"box", {std::lround(b.x), std::lround(b.y), std::lround(b.w), std::lround(b.h)}},
                        {"score", b.score},
                        {"reading", reading}});
    }
    return plates;
  }

 private:
  std::vector<Box> detect(const uint8_t* f, int W, int H) {
    int rx = std::clamp(cfg_.roi[0], 0, W - 2), ry = std::clamp(cfg_.roi[1], 0, H - 2);
    int rw = std::clamp(cfg_.roi[2], 2, W - rx), rh = std::clamp(cfg_.roi[3], 2, H - ry);
    float s = std::min(static_cast<float>(dw_) / rw, static_cast<float>(dh_) / rh);
    float px = (dw_ - rw * s) / 2, py = (dh_ - rh * s) / 2;
    InputWriter in{det_->tensor(det_->inputs()[0])};
    uint8_t rgb[3];
    for (int y = 0; y < dh_; ++y) {
      for (int x = 0; x < dw_; ++x) {
        size_t i = (static_cast<size_t>(y) * dw_ + x) * 3;
        float sx = (x + 0.5f - px) / s, sy = (y + 0.5f - py) / s;
        if (sx < 0 || sy < 0 || sx >= rw || sy >= rh) {
          for (int c = 0; c < 3; ++c) in.set(i + c, 114.f / 255.f);
          continue;
        }
        nv12_rgb(f, W, H, rx + static_cast<int>(sx), ry + static_cast<int>(sy), rgb);
        for (int c = 0; c < 3; ++c) in.set(i + c, rgb[c] / 255.f);
      }
    }
    if (det_->Invoke() != kTfLiteOk) throw std::runtime_error("detector invoke failed");
    const TfLiteTensor* out = det_->tensor(det_->outputs()[0]);
    if (out->dims->size != 3) throw std::runtime_error("detector output must be rank 3");
    int a = out->dims->data[1], b = out->dims->data[2];
    bool channels_first = a < b;  // [1,4+C,N] (ultralytics) vs [1,N,4+C]
    int attrs = channels_first ? a : b, n = channels_first ? b : a;
    if (attrs < 5) throw std::runtime_error("detector output has no class scores");
    auto at = [&](int k, int j) { return read_out(out, channels_first ? static_cast<size_t>(k) * n + j : static_cast<size_t>(j) * attrs + k); };
    std::vector<Box> cand;
    float maxc = 0;
    for (int j = 0; j < n; ++j) {
      float score = 0;
      for (int k = 4; k < attrs; ++k) score = std::max(score, at(k, j));
      if (score < cfg_.score) continue;
      Box bx{at(0, j), at(1, j), at(2, j), at(3, j), score};
      maxc = std::max({maxc, bx.x, bx.y});
      cand.push_back(bx);
    }
    // Ultralytics TFLite exports normalized xywh; scale to input pixels if so.
    bool normalized = maxc <= 1.5f;
    for (auto& bx : cand) {
      if (normalized) { bx.x *= dw_; bx.w *= dw_; bx.y *= dh_; bx.h *= dh_; }
      float x0 = (bx.x - bx.w / 2 - px) / s + rx, y0 = (bx.y - bx.h / 2 - py) / s + ry;
      bx = Box{x0, y0, bx.w / s, bx.h / s, bx.score};
    }
    std::sort(cand.begin(), cand.end(), [](const Box& l, const Box& r) { return l.score > r.score; });
    std::vector<Box> keep;
    for (const auto& bx : cand) {
      bool dup = false;
      for (const auto& k : keep) dup = dup || iou(bx, k) > cfg_.iou;
      if (!dup) keep.push_back(bx);
      if (static_cast<int>(keep.size()) >= cfg_.max_plates) break;
    }
    return keep;
  }

  json read(const uint8_t* f, int W, int H, const Box& b) {
    float mx = b.w * cfg_.margin, my = b.h * cfg_.margin;
    float x0 = b.x - mx, y0 = b.y - my, cw = b.w + 2 * mx, ch = b.h + 2 * my;
    InputWriter in{ocr_->tensor(ocr_->inputs()[0])};
    uint8_t rgb[3];
    for (int y = 0; y < oh_; ++y) {
      for (int x = 0; x < ow_; ++x) {
        float fx = x0 + (x + 0.5f) * cw / ow_, fy = y0 + (y + 0.5f) * ch / oh_;
        size_t i = (static_cast<size_t>(y) * ow_ + x) * oc_;
        if (oc_ == 1) {
          in.set(i, nv12_gray(f, W, H, fx, fy) / 255.f);
        } else {
          nv12_rgb(f, W, H, static_cast<int>(fx), static_cast<int>(fy), rgb);
          for (int c = 0; c < 3; ++c) in.set(i + c, rgb[c] / 255.f);
        }
      }
    }
    if (ocr_->Invoke() != kTfLiteOk) throw std::runtime_error("reader invoke failed");
    const TfLiteTensor* out = ocr_->tensor(ocr_->outputs()[0]);
    // The serial number decides the record confidence. Other fields are
    // blanked (= unread, never matching a rule) when any of their heads is
    // below fieldThreshold, so a guessed region cannot trigger a condition.
    std::string text[4];
    float field_conf[4] = {1.f, 1.f, 1.f, 1.f};
    auto field_index = [](const std::string& f) { return f == "region" ? 0 : f == "class" ? 1 : f == "kana" ? 2 : 3; };
    size_t offset = 0;
    for (const auto& h : cfg_.heads) {
      size_t m = h.labels.size();
      float mx_logit = -1e30f;
      size_t best = 0;
      for (size_t k = 0; k < m; ++k) {
        float v = read_out(out, offset + k);
        if (v > mx_logit) { mx_logit = v; best = k; }
      }
      float sum = 0;
      for (size_t k = 0; k < m; ++k) sum += std::exp(read_out(out, offset + k) - mx_logit);
      int fi = field_index(h.field);
      field_conf[fi] = std::min(field_conf[fi], 1.f / sum);
      text[fi] += h.labels[best];
      offset += m;
    }
    for (int fi = 0; fi < 3; ++fi)
      if (field_conf[fi] < cfg_.field_threshold) text[fi].clear();
    return {{"region", text[0]}, {"class", text[1]}, {"kana", text[2]}, {"number", text[3]}, {"confidence", field_conf[3]}};
  }

  Config cfg_;
  std::unique_ptr<tflite::FlatBufferModel> det_model_, ocr_model_;
  std::unique_ptr<tflite::Interpreter> det_, ocr_;
  int dw_ = 0, dh_ = 0, ow_ = 0, oh_ = 0, oc_ = 0;
};

Config load_config(const std::string& path) {
  std::ifstream in(path);
  if (!in) throw std::runtime_error("cannot open config");
  json j = json::parse(in);
  if (j.value("version", 0) != 1) throw std::runtime_error("unsupported config version");
  Config c;
  if (j.contains("detector")) {
    auto& d = j["detector"];
    c.score = d.value("scoreThreshold", c.score);
    c.iou = d.value("iouThreshold", c.iou);
    c.max_plates = d.value("maxPlates", c.max_plates);
    c.min_width = d.value("minWidth", c.min_width);
    if (d.contains("roi")) for (int i = 0; i < 4; ++i) c.roi[i] = d["roi"].at(i).get<int>();
  }
  auto& o = j.at("ocr");
  c.margin = o.value("margin", c.margin);
  c.field_threshold = o.value("fieldThreshold", c.field_threshold);
  for (auto& h : o.at("heads")) {
    Head head{h.at("field").get<std::string>(), h.at("labels").get<std::vector<std::string>>()};
    if (head.field != "region" && head.field != "class" && head.field != "kana" && head.field != "number")
      throw std::runtime_error("unknown head field " + head.field);
    if (head.labels.empty()) throw std::runtime_error("empty head");
    c.heads.push_back(head);
  }
  if (c.heads.empty()) throw std::runtime_error("no reader heads");
  return c;
}

bool read_exact(void* p, size_t n) { return std::fread(p, 1, n, stdin) == n; }

void emit(const json& j) {
  std::string s = j.dump(-1, ' ', false, json::error_handler_t::replace);
  s.push_back('\n');
  std::fwrite(s.data(), 1, s.size(), stdout);
  std::fflush(stdout);
}

}  // namespace

int main(int argc, char** argv) {
  std::string det, ocr, conf;
  int threads = 2, bench = 0;
  for (int i = 1; i + 1 < argc; i += 2) {
    std::string k = argv[i], v = argv[i + 1];
    if (k == "--detector") det = v;
    else if (k == "--ocr") ocr = v;
    else if (k == "--config") conf = v;
    else if (k == "--threads") threads = std::clamp(std::atoi(v.c_str()), 1, 4);
    else if (k == "--bench") bench = std::clamp(std::atoi(v.c_str()), 1, 1000);
  }
  std::unique_ptr<Engine> engine;
  try {
    if (det.empty() || ocr.empty() || conf.empty()) throw std::runtime_error("usage: --detector D --ocr O --config C [--threads N]");
    engine = std::make_unique<Engine>(det, ocr, load_config(conf), threads);
  } catch (const std::exception& e) {
    emit({{"ready", false}, {"error", e.what()}});
    return 1;
  }
  emit({{"ready", true}, {"engine", "tflite " TFLITE_VERSION_LABEL}, {"backend", "xnnpack cpu x" + std::to_string(threads)}});
  if (bench) {
    emit({{"bench", engine->bench(bench)}});
    return 0;
  }
  std::vector<uint8_t> frame;
  uint8_t header[24];
  while (read_exact(header, sizeof header)) {
    uint32_t w, h, fmt;
    uint64_t seq;
    std::memcpy(&w, header + 4, 4);
    std::memcpy(&h, header + 8, 4);
    std::memcpy(&fmt, header + 12, 4);
    std::memcpy(&seq, header + 16, 8);
    if (std::memcmp(header, "NPF1", 4) != 0 || fmt != 1 || w < 16 || h < 16 || w > 4096 || h > 4096 || (w | h) & 1) {
      std::fprintf(stderr, "invalid frame header\n");
      return 2;  // stream position is unknown; let the supervisor restart us
    }
    frame.resize(static_cast<size_t>(w) * h * 3 / 2);
    if (!read_exact(frame.data(), frame.size())) return 0;
    auto t0 = std::chrono::steady_clock::now();
    json reply = {{"seq", seq}};
    try {
      reply["plates"] = engine->process(frame.data(), static_cast<int>(w), static_cast<int>(h));
    } catch (const std::exception& e) {
      reply["plates"] = json::array();
      reply["error"] = e.what();
    }
    reply["ms"] = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
    emit(reply);
  }
  return 0;
}
