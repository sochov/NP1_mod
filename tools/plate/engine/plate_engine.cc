// NP1 plate-recognition inference helper.
//
// Protocol (see backend/internal/platesensor/engine.go):
//   stdin : "NPF1" | u32 width | u32 height | u32 format(1=NV12) | u64 seq | pixels
//         : "NPM1" | same header; pixels are in the negotiated shared buffer.
//   stdout: one JSON line per frame. The first line is the ready line.
// Diagnostics go to stderr only; stdout carries protocol lines exclusively.
//
// Stage 1: plate detector (YOLO-style, NHWC RGB input, output [1,4+C,N] or [1,N,4+C]).
// Stage 2: plate reader, a multi-head classifier over the fixed Japanese plate
//          layout (region / class digits / kana / serial digits).
#include <algorithm>
#include <array>
#include <chrono>
#include <cctype>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iterator>
#include <functional>
#include <future>
#include <initializer_list>
#include <limits>
#include <map>
#include <memory>
#include <numeric>
#include <optional>
#include <regex>
#include <set>
#include <stdexcept>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

#include <nlohmann/json.hpp>
#include <onnxruntime_cxx_api.h>

#if defined(__aarch64__)
#include <arm_neon.h>
#endif

#ifdef NP1_PLATE_MNN_GPU
#include <MNN/Interpreter.hpp>
#include <mutex>
#endif

#include "tensorflow/lite/interpreter.h"
#include "tensorflow/lite/kernels/register.h"
#include "tensorflow/lite/model.h"
#ifdef NP1_PLATE_GPU
#include "tensorflow/lite/delegates/gpu/delegate.h"
#endif

using json = nlohmann::json;

namespace {

FILE* protocol_output = nullptr;

struct Box {
  float x, y, w, h, score;
};

struct Head {
  std::string field;
  std::vector<std::string> labels;
};

struct Config {
  float score = 0.4f, iou = 0.5f, margin = 0.06f, field_threshold = 0.6f;
  float region_threshold = 0.6f, kana_threshold = 0.6f;
  int max_plates = 4, min_width = 24;
  std::array<int, 4> roi{0, 0, 1920, 1080};
  std::vector<std::array<int, 4>> side_rois;
  std::optional<std::array<int, 4>> fallback_roi;
  bool ignore_recorder_overlay = false;
  int full_ocr_every = 1;
  int full_ocr_min_width = 0;
  int crowded_ocr_interval = 1;
  bool reread_shrinking_digits = false;
  bool require_digit_detail = false;
  int ocr_min_crop_width = 0;
  float ocr_min_detection_score = 0.f;
  bool reread_narrow_cool_crops = false;
  bool reread_crowded_warm_crops = false;
  std::vector<Head> heads;
  std::string model_dir;
  json field_readers = json::object();
  json text_ocr;
  json numeric_verifier;
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

#if defined(__aarch64__)
// Exact 1/2-scale NEON NV12 -> NHWC float32 [0,1] for ROI 2*dw x 2*dh (e.g., 1280x768 -> 640x384).
inline void fill_det_neon_half_f32(const uint8_t* f, int W, int H, int rx, int ry,
                                   int dw, int dh, float* out) {
  const uint8_t* uv_base = f + W * H;
  float* dst = out;
  const uint8x8_t v16 = vdup_n_u8(16);
  const int16x8_t v128 = vdupq_n_s16(128);
  const float32x4_t vinv255 = vdupq_n_f32(1.0f / 255.0f);
  for (int y = 0; y < dh; ++y) {
    const int sy = ry + 2 * y + 1;
    const uint8_t* y_row = f + sy * W + rx;
    const uint8_t* uv_row = uv_base + (sy >> 1) * W + (rx & ~1);
    for (int x = 0; x + 8 <= dw; x += 8) {
      uint8x8x2_t y_pair = vld2_u8(y_row + x * 2);
      uint8x8x2_t uv_pair = vld2_u8(uv_row + x * 2);
      int16x8_t c = vreinterpretq_s16_u16(vsubl_u8(y_pair.val[1], v16));
      int16x8_t d = vsubq_s16(vreinterpretq_s16_u16(vmovl_u8(uv_pair.val[0])), v128);
      int16x8_t e = vsubq_s16(vreinterpretq_s16_u16(vmovl_u8(uv_pair.val[1])), v128);
      int32x4_t c_lo = vmlal_n_s16(vdupq_n_s32(128), vget_low_s16(c), 298);
      int32x4_t c_hi = vmlal_n_s16(vdupq_n_s32(128), vget_high_s16(c), 298);
      uint8x8_t r8 = vqmovun_s16(vcombine_s16(vshrn_n_s32(vmlal_n_s16(c_lo, vget_low_s16(e), 409), 8),
                                              vshrn_n_s32(vmlal_n_s16(c_hi, vget_high_s16(e), 409), 8)));
      uint8x8_t g8 = vqmovun_s16(vcombine_s16(
          vshrn_n_s32(vmlal_n_s16(vmlal_n_s16(c_lo, vget_low_s16(d), -100), vget_low_s16(e), -208), 8),
          vshrn_n_s32(vmlal_n_s16(vmlal_n_s16(c_hi, vget_high_s16(d), -100), vget_high_s16(e), -208), 8)));
      uint8x8_t b8 = vqmovun_s16(vcombine_s16(vshrn_n_s32(vmlal_n_s16(c_lo, vget_low_s16(d), 516), 8),
                                              vshrn_n_s32(vmlal_n_s16(c_hi, vget_high_s16(d), 516), 8)));
      uint16x8_t r16 = vmovl_u8(r8), g16 = vmovl_u8(g8), b16 = vmovl_u8(b8);
      float32x4x3_t lo, hi;
      lo.val[0] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_low_u16(r16))), vinv255);
      lo.val[1] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_low_u16(g16))), vinv255);
      lo.val[2] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_low_u16(b16))), vinv255);
      vst3q_f32(dst, lo);
      hi.val[0] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_high_u16(r16))), vinv255);
      hi.val[1] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_high_u16(g16))), vinv255);
      hi.val[2] = vmulq_f32(vcvtq_f32_u32(vmovl_u16(vget_high_u16(b16))), vinv255);
      vst3q_f32(dst + 12, hi);
      dst += 24;
    }
  }
}

inline void fill_det_neon_half_int8(const uint8_t* f, int W, int H, int rx, int ry,
                                    int dw, int dh, int8_t* out) {
  const uint8_t* uv_base = f + W * H;
  int8_t* dst = out;
  const uint8x8_t v16 = vdup_n_u8(16);
  const int16x8_t v128 = vdupq_n_s16(128);
  const uint8x8_t v0x80 = vdup_n_u8(0x80);
  for (int y = 0; y < dh; ++y) {
    const int sy = ry + 2 * y + 1;
    const uint8_t* y_row = f + sy * W + rx;
    const uint8_t* uv_row = uv_base + (sy >> 1) * W + (rx & ~1);
    for (int x = 0; x + 8 <= dw; x += 8) {
      uint8x8x2_t y_pair = vld2_u8(y_row + x * 2);
      uint8x8x2_t uv_pair = vld2_u8(uv_row + x * 2);
      int16x8_t c = vreinterpretq_s16_u16(vsubl_u8(y_pair.val[1], v16));
      int16x8_t d = vsubq_s16(vreinterpretq_s16_u16(vmovl_u8(uv_pair.val[0])), v128);
      int16x8_t e = vsubq_s16(vreinterpretq_s16_u16(vmovl_u8(uv_pair.val[1])), v128);
      int32x4_t c_lo = vmlal_n_s16(vdupq_n_s32(128), vget_low_s16(c), 298);
      int32x4_t c_hi = vmlal_n_s16(vdupq_n_s32(128), vget_high_s16(c), 298);
      uint8x8x3_t rgb;
      rgb.val[0] = veor_u8(vqmovun_s16(vcombine_s16(vshrn_n_s32(vmlal_n_s16(c_lo, vget_low_s16(e), 409), 8),
                                                    vshrn_n_s32(vmlal_n_s16(c_hi, vget_high_s16(e), 409), 8))), v0x80);
      rgb.val[1] = veor_u8(vqmovun_s16(vcombine_s16(
          vshrn_n_s32(vmlal_n_s16(vmlal_n_s16(c_lo, vget_low_s16(d), -100), vget_low_s16(e), -208), 8),
          vshrn_n_s32(vmlal_n_s16(vmlal_n_s16(c_hi, vget_high_s16(d), -100), vget_high_s16(e), -208), 8))), v0x80);
      rgb.val[2] = veor_u8(vqmovun_s16(vcombine_s16(vshrn_n_s32(vmlal_n_s16(c_lo, vget_low_s16(d), 516), 8),
                                                    vshrn_n_s32(vmlal_n_s16(c_hi, vget_high_s16(d), 516), 8))), v0x80);
      vst3_u8(reinterpret_cast<uint8_t*>(dst), rgb);
      dst += 24;
    }
  }
}
#endif

// Bilinear full-range RGB at a sub-pixel position (pixel centres at integer coordinates).
inline void nv12_rgb_bilinear(const uint8_t* f, int W, int H, float fx, float fy, float* rgb) {
  fx = std::clamp(fx, 0.f, W - 1.f);
  fy = std::clamp(fy, 0.f, H - 1.f);
  int x0 = static_cast<int>(fx), y0 = static_cast<int>(fy);
  int x1 = std::min(x0 + 1, W - 1), y1 = std::min(y0 + 1, H - 1);
  float ax = fx - x0, ay = fy - y0;
  uint8_t a[3], b[3], c[3], d[3];
  nv12_rgb(f, W, H, x0, y0, a);
  nv12_rgb(f, W, H, x1, y0, b);
  nv12_rgb(f, W, H, x0, y1, c);
  nv12_rgb(f, W, H, x1, y1, d);
  for (int k = 0; k < 3; ++k) rgb[k] = (a[k] * (1 - ax) + b[k] * ax) * (1 - ay) + (c[k] * (1 - ax) + d[k] * ax) * ay;
}

// All output rows share the same horizontal sampling coordinates. Keep the
// bilinear weights and video-range conversion identical to scalar sampling.
template<class XPosition, class YPosition, class Write>
void sample_gray_crop(const uint8_t* frame, int width, int height, int cols, int rows,
                      XPosition at_x, YPosition at_y, Write write) {
  struct Column { int x; float weight; };
  std::vector<Column> columns;
  columns.reserve(cols);
  for (int col = 0; col < cols; ++col) {
    const float fx = std::clamp(at_x(col), 0.f, width - 1.001f);
    const int x = static_cast<int>(fx);
    columns.push_back({x, fx - x});
  }
  size_t index = 0;
  for (int row = 0; row < rows; ++row) {
    const float fy = std::clamp(at_y(row), 0.f, height - 1.001f);
    const int y = static_cast<int>(fy);
    const float ay = fy - y;
    const uint8_t* line = frame + y * width;
    for (const auto& [x, ax] : columns) {
      const uint8_t* p = line + x;
      const float v = (p[0] * (1 - ax) + p[1] * ax) * (1 - ay) +
                      (p[width] * (1 - ax) + p[width + 1] * ax) * ay;
      write(index++, clamp8(static_cast<int>(std::lround((v - 16.f) * 255.f / 219.f))));
    }
  }
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

enum class GPU { off, full_graph, cached_full_graph, one_partition, cached_partition, text };

class TFLiteModel {
 public:
  TFLiteModel(const std::string& path, int threads, GPU gpu, std::string cache_dir = {}, bool prepare_cache = false, bool precision_loss = false)
      : name_(std::filesystem::path(path).filename().string()), threads_(threads),
        model_(tflite::FlatBufferModel::BuildFromFile(path.c_str())) {
    if (!model_) throw std::runtime_error("cannot load model: " + name_);
#ifdef NP1_PLATE_GPU
    bool allow_partial = gpu == GPU::one_partition || gpu == GPU::cached_partition;
    if (!cache_dir.empty()) {
      cache_dir_ = std::move(cache_dir);
      const auto* bytes = static_cast<const uint8_t*>(model_->allocation()->base());
      uint64_t fingerprint = 14695981039346656037ULL;
      for (size_t i = 0; i < model_->allocation()->bytes(); ++i)
        fingerprint = (fingerprint ^ bytes[i]) * 1099511628211ULL;
      cache_token_ = std::string(allow_partial ? "np1-detector-fp16-v1-" : "np1-ocr-fp32-v1-") +
                     std::to_string(fingerprint);
    }
    const bool has_ready_cache = !cache_dir_.empty() && std::filesystem::is_regular_file(cache_marker());
    if ((gpu == GPU::cached_partition || gpu == GPU::cached_full_graph) && !has_ready_cache) gpu = GPU::off;
    if (gpu != GPU::off) {
      TfLiteGpuDelegateOptionsV2 options = TfLiteGpuDelegateOptionsV2Default();
      options.experimental_flags = TFLITE_GPU_EXPERIMENTAL_FLAGS_CL_ONLY;
      if (gpu == GPU::text) options.is_precision_loss_allowed = precision_loss;
      if (allow_partial) options.experimental_flags |= TFLITE_GPU_EXPERIMENTAL_FLAGS_ENABLE_QUANT;
      options.inference_preference = TFLITE_GPU_INFERENCE_PREFERENCE_SUSTAINED_SPEED;
      options.inference_priority1 = (allow_partial || gpu == GPU::text) ? TFLITE_GPU_INFERENCE_PRIORITY_MIN_LATENCY
                                                 : TFLITE_GPU_INFERENCE_PRIORITY_MAX_PRECISION;
      // Only enable serialization when explicitly preparing caches or reading a verified cache,
      // so a read-only /overlay sandbox never fails trying to write new cache files at runtime.
      if (!cache_dir_.empty() && (prepare_cache || has_ready_cache)) {
        options.experimental_flags |= TFLITE_GPU_EXPERIMENTAL_FLAGS_ENABLE_SERIALIZATION;
        options.serialization_dir = cache_dir_.c_str();
        options.model_token = cache_token_.c_str();
      }
      delegate_.reset(TfLiteGpuDelegateV2Create(&options));
      if (delegate_) interpreter_ = build(delegate_.get());
      if (interpreter_ && delegated(allow_partial)) return;
      fall_back();
      return;
    }
#else
    (void)gpu;
    (void)precision_loss;
    (void)cache_dir;
    (void)prepare_cache;
#endif
    start_cpu();
  }

  tflite::Interpreter* get() const { return interpreter_.get(); }
  tflite::Interpreter* operator->() const { return get(); }
  bool uses_gpu() const {
#ifdef NP1_PLATE_GPU
    return static_cast<bool>(delegate_);
#else
    return false;
#endif
  }

  void mark_cache_ready(bool required = true) const {
#ifdef NP1_PLATE_GPU
    if (uses_gpu() && !cache_dir_.empty()) {
      // A delegate which cannot serialize a program must use the normal CPU
      // detector on startup instead of compiling from scratch each time.
      for (const auto& entry : std::filesystem::directory_iterator(cache_dir_)) {
        const auto path = entry.path();
        if (path.extension() != ".bin" || path.filename().string().find(cache_token_ + "_") != 0 ||
            !entry.is_regular_file() || entry.file_size() == 0) continue;
        std::ofstream marker(cache_marker());
        marker << "OpenCL\n" << std::flush;
        if (marker) return;
        break;
      }
    }
#endif
    if (required) throw std::runtime_error("GPU cache could not be prepared: " + name_);
  }

  template <typename Fill>
  void invoke(Fill fill) {
    fill(get());
    if (interpreter_->Invoke() == kTfLiteOk) return;
#ifdef NP1_PLATE_GPU
    if (delegate_) {
      fall_back();
      fill(get());
      if (interpreter_->Invoke() == kTfLiteOk) return;
    }
#endif
    throw std::runtime_error("invoke failed: " + name_);
  }

 private:
  std::unique_ptr<tflite::Interpreter> build(TfLiteDelegate* delegate) {
    tflite::ops::builtin::BuiltinOpResolver resolver;
    std::unique_ptr<tflite::Interpreter> interpreter;
    tflite::InterpreterBuilder builder(*model_, resolver);
    builder.SetNumThreads(threads_);
    if (delegate) builder.AddDelegate(delegate);
    if (builder(&interpreter) != kTfLiteOk || !interpreter ||
        interpreter->AllocateTensors() != kTfLiteOk) return nullptr;
    return interpreter;
  }

  void start_cpu() {
    interpreter_ = build(nullptr);
    if (!interpreter_) throw std::runtime_error("cannot initialize CPU interpreter: " + name_);
  }

#ifdef NP1_PLATE_GPU
  std::string cache_dir_, cache_token_;
  std::filesystem::path cache_marker() const {
    return std::filesystem::path(cache_dir_) / (cache_token_ + ".ready");
  }

  bool delegated(bool allow_partial) const {
    const auto& plan = interpreter_->execution_plan();
    if (!allow_partial && plan.size() != 1) return false;
    return std::any_of(plan.begin(), plan.end(), [&](int index) {
      return interpreter_->node_and_registration(index)->first.delegate == delegate_.get();
    });
  }

  void fall_back() {
    std::fprintf(stderr, "plate-engine: %s falling back to CPU\n", name_.c_str());
    interpreter_.reset();
    delegate_.reset();
    start_cpu();
  }
#endif

  std::string name_;
  int threads_;
  std::unique_ptr<tflite::FlatBufferModel> model_;
#ifdef NP1_PLATE_GPU
  std::unique_ptr<TfLiteDelegate, decltype(&TfLiteGpuDelegateV2Delete)> delegate_{nullptr, TfLiteGpuDelegateV2Delete};
#endif
  std::unique_ptr<tflite::Interpreter> interpreter_;
};

std::vector<float> softmax(const TfLiteTensor* tensor, size_t offset, size_t count) {
  std::vector<float> values(count);
  float maximum = -std::numeric_limits<float>::infinity();
  for (size_t i = 0; i < count; ++i) {
    values[i] = read_out(tensor, offset + i);
    if (!std::isfinite(values[i])) return std::vector<float>(count);
    maximum = std::max(maximum, values[i]);
  }
  double total = 0;
  for (float& value : values) {
    value = std::exp(value - maximum);
    total += value;
  }
  if (!(total > 0)) return std::vector<float>(count);
  for (float& value : values) value = static_cast<float>(value / total);
  return values;
}

std::vector<std::string> utf8_tokens(const std::string& value) {
  std::vector<std::string> out;
  for (size_t at = 0; at < value.size();) {
    unsigned char first = static_cast<unsigned char>(value[at]);
    size_t length = first < 0x80 ? 1 : (first & 0xe0) == 0xc0 ? 2 : (first & 0xf0) == 0xe0 ? 3 : 4;
    if (at + length > value.size()) throw std::runtime_error("invalid UTF-8 model metadata");
    out.push_back(value.substr(at, length));
    at += length;
  }
  return out;
}

std::string join_tokens(const std::vector<std::string>& values, size_t first = 0, size_t last = std::string::npos) {
  if (last == std::string::npos) last = values.size();
  std::string out;
  for (size_t i = first; i < std::min(last, values.size()); ++i) out += values[i];
  return out;
}

std::string canonical_token(std::string token, bool kana = false) {
  static const std::unordered_map<std::string, std::string> aliases = {
      {"户", "戸"}, {"宫", "宮"}, {"滨", "浜"}, {"濱", "浜"}, {"广", "広"},
      {"岛", "島"}, {"长", "長"}, {"泽", "沢"}, {"冈", "岡"}, {"丰", "豊"},
      {"习", "習"}, {"仓", "倉"}, {"贺", "賀"}, {"爱", "愛"}, {"张", "張"}};
  if (kana && (token == "<" || token == "〈" || token == "＜")) return "く";
  auto found = aliases.find(token);
  if (found != aliases.end()) return found->second;
  // NFKC for the characters used by the plate parser: full-width ASCII.
  auto code = utf8_tokens(token);
  if (code.size() == 1 && token.size() == 3) {
    const unsigned char* p = reinterpret_cast<const unsigned char*>(token.data());
    unsigned int cp = ((p[0] & 0x0f) << 12) | ((p[1] & 0x3f) << 6) | (p[2] & 0x3f);
    if (cp >= 0xff01 && cp <= 0xff5e) return std::string(1, static_cast<char>(cp - 0xfee0));
  }
  return token;
}

bool is_digit_token(const std::string& value) { return value.size() == 1 && value[0] >= '0' && value[0] <= '9'; }
bool is_class_token(const std::string& value) {
  return is_digit_token(value) || (value.size() == 1 && std::string("ACFHKLMPXY").find(value[0]) != std::string::npos);
}
bool is_dot_token(const std::string& value) {
  return value == "." || value == "・" || value == "·" || value == "●" || value == "。";
}
bool is_hyphen_token(const std::string& value) { return value == "-" || value == "－" || value == "‐"; }
// Hiragana / katakana / CJK ideograph: characters that can belong to a region name.
bool is_name_token(const std::string& value) {
  if (value.size() != 3) return false;
  const unsigned char* p = reinterpret_cast<const unsigned char*>(value.data());
  unsigned int cp = ((p[0] & 0x0f) << 12) | ((p[1] & 0x3f) << 6) | (p[2] & 0x3f);
  return (cp >= 0x3040 && cp <= 0x30ff) || (cp >= 0x4e00 && cp <= 0x9fff);
}

using Rect = std::array<double, 4>;

std::vector<Rect> parse_rects(const json& value) {
  std::vector<Rect> out;
  for (const auto& r : value) {
    auto v = r.get<std::vector<double>>();
    if (v.size() != 4) throw std::runtime_error("text OCR rect must contain four values");
    out.push_back({v[0], v[1], v[2], v[3]});
  }
  if (out.empty()) throw std::runtime_error("text OCR rect list is empty");
  return out;
}

struct Choice {
  std::string text;
  float confidence = 0;
  double log_probability = -std::numeric_limits<double>::infinity();
};

bool valid_region(const std::string& value, const std::set<std::string>& regions) { return regions.count(value) != 0; }
bool valid_kana(const std::string& value, const std::set<std::string>& kana) { return kana.count(value) != 0; }
bool valid_classification(const std::string& value) {
  if (value.empty() || value.size() > 3 || !is_digit_token(value.substr(0, 1))) return false;
  if (value.size() <= 2) return std::all_of(value.begin(), value.end(), [](char c) { return c >= '0' && c <= '9'; });
  return std::all_of(value.begin(), value.end(), [](char c) {
    return (c >= '0' && c <= '9') || std::string("ACFHKLMPXY").find(c) != std::string::npos;
  });
}
bool valid_serial(const std::string& value) {
  return !value.empty() && value.size() <= 4 && value[0] >= '1' && value[0] <= '9' &&
         std::all_of(value.begin(), value.end(), [](char c) { return c >= '0' && c <= '9'; });
}

Choice single_choice(const std::vector<float>& probabilities, const std::vector<std::string>& labels,
                     const std::function<bool(const std::string&)>& allowed) {
  int best = -1;
  for (size_t i = 0; i < labels.size(); ++i)
    if (allowed(labels[i]) && (best < 0 || probabilities[i] > probabilities[best])) best = static_cast<int>(i);
  if (best < 0) return {};
  float p = probabilities[best];
  return {labels[best], p, std::log(std::max(1e-30f, p))};
}

float label_probability(const std::vector<float>& probabilities, const std::vector<std::string>& labels,
                        const std::string& label) {
  for (size_t i = 0; i < labels.size(); ++i)
    if (labels[i] == label) return probabilities[i];
  return 0;
}

Choice sequence_choice(const std::string& text, const std::vector<std::string>& tokens,
                       const std::vector<std::vector<float>>& probabilities,
                       const std::vector<std::vector<std::string>>& labels) {
  float confidence = 1;
  double score = 0;
  for (size_t i = 0; i < tokens.size(); ++i) {
    float p = label_probability(probabilities[i], labels[i], tokens[i]);
    confidence = std::min(confidence, p);
    score += std::log(std::max(1e-30f, p));
  }
  return {text, confidence, score};
}

std::string best_token(const std::vector<float>& probabilities, const std::vector<std::string>& labels,
                       const std::string& allowed) {
  return single_choice(probabilities, labels, [&](const std::string& value) {
           return value.size() == 1 && allowed.find(value) != std::string::npos;
         }).text;
}

Choice serial_choice(const std::vector<std::vector<float>>& probabilities,
                     const std::vector<std::vector<std::string>>& labels) {
  Choice best;
  for (int length = 1; length <= 4; ++length) {
    std::vector<std::string> tokens(4);
    std::string number;
    int first = 4 - length;
    for (int i = first; i < 4; ++i) {
      tokens[i] = best_token(probabilities[i], labels[i], i == first ? "123456789" : "0123456789");
      number += tokens[i];
    }
    if (!valid_serial(number)) continue;
    Choice candidate = sequence_choice(number, tokens, probabilities, labels);
    if (candidate.log_probability > best.log_probability) best = candidate;
  }
  return best;
}

Choice classification_choice(const std::vector<std::vector<float>>& probabilities,
                              const std::vector<std::vector<std::string>>& labels) {
  Choice best;
  for (int length = 1; length <= 3; ++length) {
    std::vector<std::string> tokens(3);
    std::string value;
    for (int i = 0; i < length; ++i) {
      tokens[i] = best_token(probabilities[i], labels[i], i == 0 || length < 3 ? "0123456789" : "0123456789ACFHKLMPXY");
      value += tokens[i];
    }
    if (!valid_classification(value)) continue;
    Choice candidate = sequence_choice(value, tokens, probabilities, labels);
    if (candidate.log_probability > best.log_probability) best = candidate;
  }
  return best;
}

double log_add(double a, double b) {
  if (a == -std::numeric_limits<double>::infinity()) return b;
  if (b == -std::numeric_limits<double>::infinity()) return a;
  double maximum = std::max(a, b);
  return maximum + std::log1p(std::exp(-std::abs(a - b)));
}

struct LexMatch {
  std::string text;
  double likelihood = 0;
  double posterior = 0;
};

struct OCRText {
  std::vector<std::string> tokens;
  std::vector<float> scores;
  std::string value() const { return join_tokens(tokens); }
};

class CtcLexicon {
 public:
  CtcLexicon() = default;
  CtcLexicon(const std::vector<std::string>& dictionary, std::vector<std::string> names, bool kana) {
    names.erase(std::remove(names.begin(), names.end(), ""), names.end());
    std::sort(names.begin(), names.end());
    names_ = std::move(names);
    std::set<std::string> needed;
    for (const auto& name : names_)
      for (auto token : utf8_tokens(name)) needed.insert(canonical_token(token, kana));
    std::map<std::string, std::vector<int>> indices;
    for (size_t i = 1; i < dictionary.size(); ++i) {
      auto tokens = utf8_tokens(dictionary[i]);
      if (tokens.size() != 1) continue;
      std::string token = canonical_token(tokens[0], kana);
      if (needed.count(token)) indices[token].push_back(static_cast<int>(i));
    }
    parents_.push_back(0);
    characters_.push_back("");
    std::map<std::string, int> prefixes{{"", 0}};
    for (const auto& name : names_) {
      int at = 0;
      std::string prefix;
      for (auto token : utf8_tokens(name)) {
        token = canonical_token(token, kana);
        prefix += token;
        auto found = prefixes.find(prefix);
        if (found == prefixes.end()) {
          int next = static_cast<int>(parents_.size());
          prefixes[prefix] = next;
          parents_.push_back(at);
          characters_.push_back(token);
          at = next;
        } else {
          at = found->second;
        }
      }
      terminals_.push_back(at);
    }
    std::map<std::string, int> unique;
    emission_index_.resize(parents_.size());
    for (size_t i = 1; i < parents_.size(); ++i) {
      auto inserted = unique.emplace(characters_[i], static_cast<int>(unique.size()));
      emission_index_[i] = inserted.first->second;
    }
    emission_ids_.resize(unique.size());
    for (const auto& entry : unique) emission_ids_[entry.second] = indices[entry.first];
  }

  LexMatch score(const float* probabilities, int steps, int count) const {
    if (steps < 1 || count < 1 || names_.empty()) return {};
    const double negative = -std::numeric_limits<double>::infinity();
    std::vector<double> emissions(emission_ids_.size() * steps, negative), blanks(steps);
    for (size_t c = 0; c < emission_ids_.size(); ++c) {
      for (int t = 0; t < steps; ++t) {
        double sum = 0;
        for (int id : emission_ids_[c]) if (id < count) sum += probabilities[t * count + id];
        emissions[c * steps + t] = std::log(std::max(1e-30, std::min(1.0, sum)));
      }
    }
    for (int t = 0; t < steps; ++t)
      blanks[t] = std::log(std::max(1e-30f, probabilities[t * count]));
    std::vector<double> previous_blank(parents_.size(), negative), previous_label(parents_.size(), negative);
    std::vector<double> next_blank(parents_.size(), negative), next_label(parents_.size(), negative);
    previous_blank[0] = blanks[0];
    for (size_t i = 1; i < parents_.size(); ++i)
      if (parents_[i] == 0) previous_label[i] = emissions[emission_index_[i] * steps];
    for (int t = 1; t < steps; ++t) {
      next_blank[0] = previous_blank[0] + blanks[t];
      next_label[0] = negative;
      for (size_t i = 1; i < parents_.size(); ++i) {
        int parent = parents_[i];
        next_blank[i] = log_add(previous_blank[i], previous_label[i]) + blanks[t];
        double value = log_add(previous_label[i], previous_blank[parent]);
        if (parent != 0 && characters_[i] != characters_[parent]) value = log_add(value, previous_label[parent]);
        next_label[i] = value + emissions[emission_index_[i] * steps + t];
      }
      previous_blank.swap(next_blank);
      previous_label.swap(next_label);
    }
    double best = negative, total = negative;
    std::string selected;
    for (size_t word = 0; word < names_.size(); ++word) {
      int node = terminals_[word];
      double probability = log_add(previous_blank[node], previous_label[node]);
      total = log_add(total, probability);
      if (probability > best) {
        best = probability;
        selected = names_[word];
      }
    }
    if (selected.empty()) return {};
    return {selected, std::exp(best / std::max<size_t>(1, utf8_tokens(selected).size())), std::exp(best - total)};
  }

 private:
  std::vector<std::string> names_, characters_;
  std::vector<int> parents_, terminals_, emission_index_;
  std::vector<std::vector<int>> emission_ids_;
};

void fill_reader_input(tflite::Interpreter* interpreter, const uint8_t* frame, int W, int H,
                       const Box& box, const std::array<double, 4>& rect) {
  TfLiteTensor* tensor = interpreter->tensor(interpreter->inputs()[0]);
  int ih = tensor->dims->data[1], iw = tensor->dims->data[2], channels = tensor->dims->data[3];
  if (channels != 1 && channels != 3) throw std::runtime_error("aux reader channels must be 1 or 3");
  float x0 = box.x + static_cast<float>(rect[0] * box.w);
  float y0 = box.y + static_cast<float>(rect[1] * box.h);
  float cw = static_cast<float>((rect[2] - rect[0]) * box.w);
  float ch = static_cast<float>((rect[3] - rect[1]) * box.h);
  InputWriter writer{tensor};
  const auto at_x = [&](int x) { return x0 + (x + .5f) * cw / iw; };
  const auto at_y = [&](int y) { return y0 + (y + .5f) * ch / ih; };
  if (channels == 1) {
    sample_gray_crop(frame, W, H, iw, ih, at_x, at_y,
                     [&](size_t at, uint8_t level) { writer.set(at, level / 255.f); });
    return;
  }
  uint8_t rgb[3];
  for (int y = 0; y < ih; ++y) {
    for (int x = 0; x < iw; ++x) {
      const size_t at = (static_cast<size_t>(y) * iw + x) * 3;
      nv12_rgb(frame, W, H, static_cast<int>(at_x(x)), static_cast<int>(at_y(y)), rgb);
      for (int c = 0; c < 3; ++c) writer.set(at + c, rgb[c] / 255.f);
    }
  }
}

class AuxReader {
 public:
  AuxReader(const std::string& path, const json& config, std::vector<Head> heads,
            int threads, bool use_gpu, const std::string& cache_dir = {}, bool prepare_cache = false)
      : heads_(std::move(heads)),
        model_(path, threads, use_gpu ? GPU::full_graph : GPU::off, cache_dir, prepare_cache) {
    auto values = config.at("rect").get<std::vector<double>>();
    if (values.size() != 4) throw std::runtime_error("aux reader rect must contain four values");
    std::copy(values.begin(), values.end(), rect_.begin());
    size_t expected = 0;
    for (const auto& head : heads_) expected += head.labels.size();
    const TfLiteTensor* output = model_->tensor(model_->outputs()[0]);
    size_t actual = 1;
    for (int i = 0; i < output->dims->size; ++i) actual *= output->dims->data[i];
    if (actual != expected) throw std::runtime_error("aux reader output size does not match configured heads");
  }

  bool uses_gpu() const { return model_.uses_gpu(); }
  void mark_cache_ready() const { model_.mark_cache_ready(false); }

  Choice read(const uint8_t* frame, int W, int H, const Box& box) {
    model_.invoke([&](auto* interpreter) { fill_reader_input(interpreter, frame, W, H, box, rect_); });
    const TfLiteTensor* output = model_->tensor(model_->outputs()[0]);
    std::vector<std::vector<float>> probabilities;
    std::vector<std::vector<std::string>> labels;
    size_t offset = 0;
    for (const auto& head : heads_) {
      probabilities.push_back(softmax(output, offset, head.labels.size()));
      labels.push_back(head.labels);
      offset += head.labels.size();
    }
    if (heads_.size() == 1 && heads_[0].field == "kana")
      return single_choice(probabilities[0], labels[0], [](const std::string& value) {
        static const std::string kana = "あいうえかきくけこをさすせそたちつてとなにぬねのはひふほまみむめもやゆよらりるろわれ";
        return !value.empty() && kana.find(value) != std::string::npos;
      });
    return classification_choice(probabilities, labels);
  }

 private:
  std::array<double, 4> rect_{};
  std::vector<Head> heads_;
  TFLiteModel model_;
};

using ProfileClock = std::chrono::steady_clock;

double elapsed_ms(ProfileClock::time_point start) {
  return std::chrono::duration<double, std::milli>(ProfileClock::now() - start).count();
}

struct FrameProfile {
  double detection = 0, primary_ocr = 0, auxiliary_ocr = 0, numeric_verification = 0;
  double digit_localization = 0, digit_localization_retry = 0;
  double text = 0, input = 0, onnx = 0;
  int calls = 0, candidates = 0, ocr_reads = 0;
  int digit_localization_retries = 0, digit_localization_rescues = 0;
  json to_json() const {
    return {{"detectorMs", detection}, {"tfliteReaderMs", primary_ocr + auxiliary_ocr + numeric_verification},
            {"primaryOcrMs", primary_ocr}, {"auxiliaryOcrMs", auxiliary_ocr},
            {"numericVerificationMs", numeric_verification}, {"ocrReads", ocr_reads}, {"textReaderMs", text},
            {"digitLocalizationMs", digit_localization}, {"digitLocalizationRetryMs", digit_localization_retry},
            {"digitLocalizationRetries", digit_localization_retries}, {"digitLocalizationRescues", digit_localization_rescues},
            {"onnxInputMs", input}, {"onnxRunMs", onnx}, {"onnxCalls", calls},
            {"plateCandidates", candidates}};
  }
};

#ifdef NP1_PLATE_MNN_GPU
uint64_t file_fingerprint(const std::string& path) {
  std::ifstream in(path, std::ios::binary);
  if (!in) throw std::runtime_error("cannot hash model: " + path);
  uint64_t hash = 14695981039346656037ULL;
  char buf[65536];
  while (in.read(buf, sizeof(buf)) || in.gcount() > 0) {
    const auto n = static_cast<size_t>(in.gcount());
    for (size_t i = 0; i < n; ++i) hash = (hash ^ static_cast<uint8_t>(buf[i])) * 1099511628211ULL;
  }
  return hash;
}

// Cached fixed-width MNN OpenCL batch runner for Adreno PP-OCR acceleration.
// In normal operation it loads only from an already-prepared cache file so it
// never blocks startup or attempts writes inside a read-only /overlay sandbox.
class MnnGpuBatch {
 public:
  MnnGpuBatch(const std::string& model_path, const std::string& cache_dir,
              int batch, int width, size_t labels, bool prepare_cache)
      : batch_(batch), width_(width), labels_(labels), prepare_cache_(prepare_cache),
        model_(MNN::Interpreter::createFromFile(model_path.c_str())) {
    if (!model_) throw std::runtime_error("cannot open MNN GPU model: " + model_path);
    if (cache_dir.empty()) throw std::runtime_error("MNN GPU requires cache directory");
    const std::string token = "np1-ppocr-w8-b" + std::to_string(batch_) + "-w" + std::to_string(width_) +
                              "-v1-" + std::to_string(file_fingerprint(model_path));
    cache_file_ = (std::filesystem::path(cache_dir) / (token + ".mnncache")).string();
    cache_marker_ = (std::filesystem::path(cache_dir) / (token + ".ready")).string();
    if (!prepare_cache_) {
      if (!std::filesystem::is_regular_file(cache_marker_) || !std::filesystem::is_regular_file(cache_file_)) {
        throw std::runtime_error("MNN GPU cache not prepared");
      }
      const auto actual_size = std::filesystem::file_size(cache_file_);
      if (actual_size == 0) throw std::runtime_error("MNN GPU cache is empty");
      std::ifstream marker(cache_marker_);
      std::string tag;
      uintmax_t expected_size = 0;
      uint64_t expected_hash = 0;
      if (!(marker >> tag >> expected_size >> expected_hash) || tag != "MNN-OpenCL-v1" ||
          expected_size != actual_size || expected_hash != file_fingerprint(cache_file_)) {
        throw std::runtime_error("MNN GPU cache integrity check failed");
      }
    }
    model_->setCacheFile(cache_file_.c_str());

    MNN::BackendConfig bcfg;
    bcfg.precision = MNN::BackendConfig::Precision_Low;
    bcfg.power = MNN::BackendConfig::Power_High;

    MNN::ScheduleConfig scfg;
    scfg.type = MNN_FORWARD_OPENCL;
    scfg.backupType = MNN_FORWARD_CPU;
    scfg.numThread = MNN_GPU_TUNING_FAST | MNN_GPU_RECORD_BATCH | MNN_GPU_MEMORY_IMAGE;
    scfg.backendConfig = &bcfg;

    model_->setSessionMode(MNN::Interpreter::Session_Resize_Defer);
    session_ = model_->createSession(scfg);
    if (!session_) throw std::runtime_error("cannot create MNN OpenCL session");

    input_ = model_->getSessionInput(session_, nullptr);
    model_->resizeTensor(input_, {batch_, 3, 48, width_});
    model_->resizeSession(session_);
    output_ = model_->getSessionOutput(session_, nullptr);
    steps_ = (width_ + 3) / 8;
    if (!output_ || output_->shape() != std::vector<int>{batch_, steps_, static_cast<int>(labels_)}) {
      throw std::runtime_error("unexpected MNN PP-OCR output shape");
    }
    cpu_in_ = std::make_unique<MNN::Tensor>(input_, MNN::Tensor::CAFFE);
    cpu_out_ = std::make_unique<MNN::Tensor>(output_, MNN::Tensor::CAFFE);

    std::array<int, 3> backends{-1, -1, -1};
    model_->getSessionInfo(session_, MNN::Interpreter::BACKENDS, backends.data());
    if (backends[0] != MNN_FORWARD_OPENCL || backends[1] != -1) {
      throw std::runtime_error("MNN PP-OCR graph did not stay fully on OpenCL");
    }
    if (prepare_cache_) model_->updateCacheFile(session_);
    std::fill_n(cpu_in_->host<float>(), cpu_in_->elementSize(), 0.0f);
    input_->copyFromHostTensor(cpu_in_.get());
    if (model_->runSession(session_) != MNN::NO_ERROR) throw std::runtime_error("MNN GPU warmup failed");
    output_->copyToHostTensor(cpu_out_.get());
    if (prepare_cache_) model_->updateCacheFile(session_);
  }

  int batch() const { return batch_; }
  int width() const { return width_; }
  int steps() const { return steps_; }

  void mark_cache_ready() const {
    if (std::filesystem::is_regular_file(cache_file_)) {
      const auto size = std::filesystem::file_size(cache_file_);
      if (size > 0) {
        std::ofstream marker(cache_marker_);
        marker << "MNN-OpenCL-v1 " << size << ' ' << file_fingerprint(cache_file_) << '\n' << std::flush;
        if (marker) return;
      }
    }
    throw std::runtime_error("PP-OCR GPU cache could not be prepared");
  }

  std::vector<float> run_batch(const std::vector<std::vector<float>>& inputs) {
    std::lock_guard<std::mutex> lk(mu_);
    const size_t one_in = static_cast<size_t>(3) * 48 * width_;
    const size_t one_out = static_cast<size_t>(steps_) * labels_;
    std::vector<float> out(inputs.size() * one_out);
    float* dst = cpu_in_->host<float>();
    const size_t bsz = static_cast<size_t>(batch_);
    for (size_t offset = 0; offset < inputs.size(); offset += bsz) {
      const size_t count = std::min(bsz, inputs.size() - offset);
      for (size_t i = 0; i < count; ++i) {
        std::memcpy(dst + i * one_in, inputs[offset + i].data(), one_in * sizeof(float));
      }
      for (size_t i = count; i < bsz; ++i) {
        std::memset(dst + i * one_in, 0, one_in * sizeof(float));
      }
      input_->copyFromHostTensor(cpu_in_.get());
      if (model_->runSession(session_) != MNN::NO_ERROR) throw std::runtime_error("MNN GPU inference failed");
      output_->copyToHostTensor(cpu_out_.get());
      std::memcpy(out.data() + offset * one_out, cpu_out_->host<float>(), count * one_out * sizeof(float));
    }
    return out;
  }

 private:
  int batch_, width_, steps_;
  size_t labels_;
  bool prepare_cache_;
  std::string cache_file_, cache_marker_;
  std::unique_ptr<MNN::Interpreter> model_;
  MNN::Session* session_ = nullptr;
  MNN::Tensor* input_ = nullptr;
  MNN::Tensor* output_ = nullptr;
  std::unique_ptr<MNN::Tensor> cpu_in_, cpu_out_;
  std::mutex mu_;
};
#endif

// SHA-256 for optional model files, checked before opening either file.
void verify_sha256(const std::string& path, const std::string& expected) {
  static const uint32_t k[64] = {
    0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
  std::ifstream in(path, std::ios::binary);
  if (!in) throw std::runtime_error("cannot open file for SHA-256: " + path);
  uint32_t h[8] = {0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};
  auto rotate = [](uint32_t x, int n) { return (x >> n) | (x << (32 - n)); };
  auto block = [&](const uint8_t* b) {
    uint32_t w[64];
    for (int i = 0; i < 16; ++i) w[i] = uint32_t(b[4*i]) << 24 | uint32_t(b[4*i+1]) << 16 | uint32_t(b[4*i+2]) << 8 | b[4*i+3];
    for (int i = 16; i < 64; ++i) w[i] = w[i-16] + (rotate(w[i-15],7) ^ rotate(w[i-15],18) ^ (w[i-15] >> 3)) + w[i-7] + (rotate(w[i-2],17) ^ rotate(w[i-2],19) ^ (w[i-2] >> 10));
    uint32_t a=h[0], b0=h[1], c=h[2], d=h[3], e=h[4], f=h[5], g=h[6], h0=h[7];
    for (int i = 0; i < 64; ++i) {
      uint32_t t1 = h0 + (rotate(e,6)^rotate(e,11)^rotate(e,25)) + ((e&f)^(~e&g)) + k[i] + w[i];
      uint32_t t2 = (rotate(a,2)^rotate(a,13)^rotate(a,22)) + ((a&b0)^(a&c)^(b0&c));
      h0=g; g=f; f=e; e=d+t1; d=c; c=b0; b0=a; a=t1+t2;
    }
    h[0]+=a; h[1]+=b0; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=h0;
  };
  uint8_t buf[128] = {}; uint64_t bytes = 0;
  while (in.read(reinterpret_cast<char*>(buf), 64)) { block(buf); bytes += 64; }
  if (!in.eof()) throw std::runtime_error("cannot read file for SHA-256: " + path);
  size_t n = static_cast<size_t>(in.gcount()); bytes += n; buf[n++] = 0x80;
  size_t end = n <= 56 ? 64 : 128;
  std::fill(buf + n, buf + end, 0);
  uint64_t bits = bytes * 8;
  for (int i = 0; i < 8; ++i) buf[end-1-i] = static_cast<uint8_t>(bits >> (8*i));
  block(buf); if (end == 128) block(buf + 64);
  char digest[65];
  for (int i = 0; i < 8; ++i) std::snprintf(digest + 8*i, 9, "%08x", h[i]);
  std::string normalized = expected;
  std::transform(normalized.begin(), normalized.end(), normalized.begin(), [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
  if (normalized != digest) throw std::runtime_error("SHA-256 mismatch: " + path);
}

class TextOCR {
 public:
  TextOCR(const std::string& model_path, const std::string& fast_path, const std::string& dictionary_path, const std::string& model_dir, const json& config,
          const std::vector<std::string>& regions, const std::vector<std::string>& kana,
          bool use_gpu = false, const std::string& gpu_cache = {}, bool prepare_cache = false,
          float min_confidence = 0.3f)
      : threads_(std::clamp(config.value("cpuThreads", 2), 1, 4)),
        min_confidence_(std::clamp(min_confidence, 0.0f, 0.99f)),
        regions_(regions.begin(), regions.end()), kana_(kana.begin(), kana.end()) {
    std::ifstream dictionary_file(dictionary_path);
    if (!dictionary_file) throw std::runtime_error("cannot open PP-OCR dictionary");
    main_.dictionary = json::parse(dictionary_file).get<std::vector<std::string>>();
    if (main_.dictionary.empty()) throw std::runtime_error("PP-OCR dictionary is empty");
    region_threshold_ = config.value("regionThreshold", .6);
    kana_threshold_ = config.value("kanaThreshold", .6);
    night_fallback_ = config.value("nightFallback", false);
    env_ = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "np1-plate");
    main_.load(*env_, threads_, model_path, fast_path, config, false);
#ifdef NP1_PLATE_MNN_GPU
    const char* ppocr_env = std::getenv("NCC_PLATE_PPOCR");
    const bool custom_crop = config.contains("sampling") || config.contains("padWidths") || config.contains("gpu");
    const bool allow_gpu = use_gpu && !custom_crop && (!ppocr_env || (std::strcmp(ppocr_env, "original") != 0 && std::strcmp(ppocr_env, "cpu") != 0));
    const auto gpu_file = config.value("gpuFile", std::string{});
    if (allow_gpu && !gpu_file.empty() && !gpu_cache.empty()) {
      const auto gpu_path = (std::filesystem::path(dictionary_path).parent_path() / gpu_file).string();
      if (std::filesystem::is_regular_file(gpu_path)) {
        const int gpu_width = std::clamp(config.value("gpuWidth", 64), 32, 128);
        const int gpu_batch = std::clamp(config.value("gpuBatch", 3), 1, 4);
        try {
          gpu_batch_ = std::make_unique<MnnGpuBatch>(gpu_path, gpu_cache, gpu_batch, gpu_width,
                                                     main_.dictionary.size(), prepare_cache);
        } catch (const std::exception& error) {
          if (prepare_cache) throw;
          std::fprintf(stderr, "plate-engine: PP-OCR GPU disabled, using CPU: %s\n", error.what());
          gpu_batch_.reset();
        }
      }
    }
#else
    (void)use_gpu;
    (void)gpu_cache;
    (void)prepare_cache;
#endif
    configure_acceleration(config, model_dir, main_);
    region_lexicon_ = CtcLexicon(main_.dictionary, regions, false);
    kana_lexicon_ = CtcLexicon(main_.dictionary, kana, true);
    // Optional behaviour (absent keys keep the original engine behaviour).
    number_check_ = config.value("numberCheck", true);
    number_score_ = config.value("numberScore", static_cast<double>(.98f));
    number_override_ = config.value("numberOverride", true);
    number_override_confidence_enabled_ = config.contains("numberOverrideConfidence");
    number_override_confidence_ = config.value("numberOverrideConfidence", 1.0);
    number_veto_score_ = config.value("numberVetoScore", number_score_);
    if ((number_override_confidence_enabled_ && !(number_override_confidence_ >= 0.0 && number_override_confidence_ <= 1.0)) ||
        (config.contains("numberVetoScore") && !(number_veto_score_ >= 0.0 && number_veto_score_ <= number_score_)))
      throw std::runtime_error("invalid numberOverrideConfidence or numberVetoScore");
    main_.bilinear = config.value("sampling", std::string("nearest")) == "bilinear";
    kana_rects_ = config.contains("kanaRects") ? parse_rects(config.at("kanaRects"))
                                               : std::vector<Rect>{{{.02, .52, .195, .84}}, {{.015, .47, .215, .88}}, {{.04, .42, .18, .95}}};
    if (number_check_ && config.contains("numberModel")) {
      const auto& spec = config.at("numberModel");
      auto dir = std::filesystem::path(model_dir);
      auto model = (dir / spec.at("file").get<std::string>()).string();
      auto dictionary = (dir / spec.at("dictionary").get<std::string>()).string();
      verify_sha256(model, spec.at("sha256").get<std::string>());
      verify_sha256(dictionary, spec.at("dictionarySha256").get<std::string>());
      number_ = std::make_unique<Recognizer>();
      auto& rec = *number_;
      std::ifstream file(dictionary);
      if (!file) throw std::runtime_error("cannot open PP-OCR dictionary");
      rec.dictionary = json::parse(file).get<std::vector<std::string>>();
      if (rec.dictionary.empty()) throw std::runtime_error("PP-OCR dictionary is empty");
      configure_acceleration(spec, model_dir, rec);
      rec.bilinear = spec.value("sampling", std::string("nearest")) == "bilinear";
      auto fast = spec.value("fastFile", std::string{});
      if (!fast.empty()) fast = (dir / fast).string();
      rec.load(*env_, threads_, model, fast, spec, true);
    }
    if (config.contains("topRow")) {
      const auto& t = config.at("topRow");
      top_rects_ = parse_rects(t.at("rects"));
      top_posterior_ = t.value("posterior", .9);
      top_gap_ = t.value("gap", 4.);
      top_max_cost_ = t.value("maxCostPerCharacter", 4.);
      top_class_minimum_ = t.value("classMinimum", .6);
      top_class_share_ = t.value("classShare", .8);
      top_class_fill_ = t.value("fillClass", true);
      if (t.contains("classOverride")) {
        top_class_override_ = t.at("classOverride").get<double>();
        top_class_override_enabled_ = true;
      }
      for (const auto& name : regions) {
        if (name.empty()) continue;
        std::vector<std::string> tokens;
        for (auto token : utf8_tokens(name)) { tokens.push_back(canonical_token(token)); region_chars_.insert(tokens.back()); }
        region_names_.push_back({name, tokens});
      }
      for (size_t i = 1; i < main_.dictionary.size(); ++i) {
        auto tokens = utf8_tokens(main_.dictionary[i]);
        if (tokens.size() == 1) token_ids_[canonical_token(tokens[0])].push_back(static_cast<int>(i));
      }
    }
  }

  bool gpu_active() const { return !main_.gpu.empty() || (number_ && !number_->gpu.empty()); }

  bool uses_int8() const { return !main_.fallback_path.empty(); }
  bool uses_gpu() const {
#ifdef NP1_PLATE_MNN_GPU
    return static_cast<bool>(gpu_batch_);
#else
    return false;
#endif
  }

  void mark_gpu_cache_ready() const {
#ifdef NP1_PLATE_MNN_GPU
    if (gpu_batch_) gpu_batch_->mark_cache_ready();
#endif
  }

  template <typename ReadKana, typename ReadClass>
  json refine(const uint8_t* frame, int W, int H, const Box& box,
              const Choice& base_class, const Choice& base_kana, Choice base_serial,
              ReadKana read_kana, ReadClass read_class, FrameProfile* profile, bool number_only) {
    profile_ = profile;
    struct ProfileReset {
      FrameProfile*& target;
      ~ProfileReset() { target = nullptr; }
    } reset{profile_};
    if (!main_.session) throw std::runtime_error("PP-OCR session unavailable");
    const auto recognize = [&] {
      return refine_once(frame, W, H, box, base_class, base_kana, base_serial, read_kana, read_class, number_only);
    };
    for (;;) {
      try {
        return recognize();
      } catch (const Ort::Exception& error) {
        auto* rec = failed_rec_;
        if (!rec || rec->fallback_path.empty()) throw;
        std::fprintf(stderr, "plate-engine: %sPP-OCR INT8 inference failed: %s\n",
                     rec == &main_ ? "" : "number ", error.what());
        const auto original = std::exchange(rec->fallback_path, {});
        rec->open(*env_, threads_, original);
      }
#ifdef NP1_PLATE_MNN_GPU
      catch (const std::exception& error) {
        if (!gpu_batch_) throw;
        std::fprintf(stderr, "plate-engine: PP-OCR GPU inference failed, falling back to CPU: %s\n", error.what());
        gpu_batch_.reset();
      }
#endif
    }
  }

 private:
  struct Candidate { std::string text; float score = 0; };
  struct Recognizer {
    std::unique_ptr<Ort::Session> session;
    std::string input_name, output_name, fallback_path;
    std::vector<std::string> dictionary;
    bool bilinear = false;
    int fast_max_width = 512;
    std::vector<int> pad_widths;
    std::map<int, std::unique_ptr<TFLiteModel>> gpu;

    void open(Ort::Env& env, int threads, const std::string& path) {
      Ort::SessionOptions options;
      options.SetIntraOpNumThreads(threads);
      options.SetInterOpNumThreads(1);
      options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);
      session = std::make_unique<Ort::Session>(env, path.c_str(), options);
      Ort::AllocatorWithDefaultOptions allocator;
      input_name = session->GetInputNameAllocated(0, allocator).get();
      output_name = session->GetOutputNameAllocated(0, allocator).get();
    }

    void load(Ort::Env& env, int threads, const std::string& original, const std::string& fast,
              const json& spec, bool number) {
      fast_max_width = std::clamp(spec.value("fastMaxInputWidth", 512), 64, 512);
      const char* mode = std::getenv("NCC_PLATE_PPOCR");
      if ((!mode || std::strcmp(mode, "original") != 0) && !fast.empty() && std::filesystem::is_regular_file(fast)) {
        if (number) verify_sha256(fast, spec.at("fastSha256").get<std::string>());
        try {
          open(env, threads, fast);
          fallback_path = original;
        } catch (const Ort::Exception& error) {
          std::fprintf(stderr, "plate-engine: %sPP-OCR INT8 initialization failed: %s\n", number ? "number " : "", error.what());
          session.reset();
        }
      }
      if (!session) open(env, threads, original);
    }
  };

  static void configure_acceleration(const json& config, const std::string& dir, Recognizer& rec) {
    auto& widths = rec.pad_widths;
    auto& models = rec.gpu;
    if (config.contains("padWidths")) {
      widths = config.at("padWidths").get<std::vector<int>>();
      if (widths.empty()) throw std::runtime_error("padWidths must not be empty");
      int previous = 0;
      for (int width : widths) {
        if (width <= previous)
          throw std::runtime_error("padWidths must contain positive ascending integers");
        previous = width;
      }
    }
    if (!config.contains("gpu")) return;
    if (widths.empty()) throw std::runtime_error("gpu requires padWidths");
    const auto& gpu = config.at("gpu");
    bool precision = gpu.value("precisionLossAllowed", false);
    std::set<int> seen;
    for (const auto& spec : gpu.at("models")) {
      int width = spec.at("width").get<int>();
      if (!std::binary_search(widths.begin(), widths.end(), width) || !seen.insert(width).second)
        throw std::runtime_error("gpu model width must match a unique padWidths bucket");
      auto path = (std::filesystem::path(dir) / spec.at("file").get<std::string>()).string();
      verify_sha256(path, spec.at("sha256").get<std::string>());
#ifdef NP1_PLATE_GPU
      std::unique_ptr<TFLiteModel> item;
      try {
        item = std::make_unique<TFLiteModel>(path, 2, GPU::text, "", false, precision);
      } catch (const std::runtime_error&) {
        std::fprintf(stderr, "PP-OCR GPU unavailable for width %d; using ONNX CPU\n", width);
        continue;
      }
      if (item->uses_gpu()) {
        auto* interpreter = item->get();
        if (interpreter->inputs().size() != 1 || interpreter->outputs().size() != 1)
          throw std::runtime_error("PP-OCR GPU model must have one input and output");
        auto* in = interpreter->tensor(interpreter->inputs()[0]);
        auto* out = interpreter->tensor(interpreter->outputs()[0]);
        if (in->type != kTfLiteFloat32 || in->dims->size != 4 || in->dims->data[0] != 1 ||
            in->dims->data[1] != 48 || in->dims->data[2] != width || in->dims->data[3] != 3)
          throw std::runtime_error("PP-OCR GPU input shape mismatch");
        if (out->type != kTfLiteFloat32 || out->dims->size != 3 || out->dims->data[0] != 1 ||
            out->dims->data[1] < 0 || out->dims->data[2] != static_cast<int>(rec.dictionary.size()))
          throw std::runtime_error("PP-OCR output shape does not match dictionary");
        models.emplace(width, std::move(item));
        continue;
      }
#else
      (void)precision;
      (void)models;
#endif
      std::fprintf(stderr, "PP-OCR GPU unavailable for width %d; using ONNX CPU\n", width);
    }
  }
  struct Output {
    const float* values = nullptr;
    int steps = 0;
    size_t classes = 0;
    Ort::Value owner{nullptr};
  };

  Output infer(Recognizer& rec, const uint8_t* frame, int W, int H, const Box& box,
               const std::array<double, 4>& rect) {
    std::vector<float> input;
    int width = 0, bucket = 0;
    const int max_width = rec.fallback_path.empty() ? 512 : rec.fast_max_width;
    const auto start = ProfileClock::now();
    const auto& widths = rec.pad_widths;
    auto select_width = [&] {
      if (widths.empty()) { bucket = width; return; }
      auto it = std::lower_bound(widths.begin(), widths.end(), width);
      bucket = it == widths.end() ? widths.back() : *it;
      width = std::min(width, bucket);
    };
    if (rec.bilinear) {
      // Sub-pixel crop of the rect (may extend past the box; clamped to the frame), bilinear resize to height 48.
      float x0 = box.x + static_cast<float>(rect[0] * box.w), y0 = box.y + static_cast<float>(rect[1] * box.h);
      float crop_width = std::max(1.f, static_cast<float>((rect[2] - rect[0]) * box.w));
      float crop_height = std::max(1.f, static_cast<float>((rect[3] - rect[1]) * box.h));
      width = std::clamp(static_cast<int>(std::lround(48.0 * crop_width / crop_height)), 8, max_width);
      select_width();
      size_t plane = static_cast<size_t>(48) * bucket;
      input.resize(3 * plane);
      float rgb[3];
      for (int y = 0; y < 48; ++y) {
        float fy = y0 + (y + .5f) * crop_height / 48.f - .5f;
        for (int x = 0; x < width; ++x) {
          nv12_rgb_bilinear(frame, W, H, x0 + (x + .5f) * crop_width / width - .5f, fy, rgb);
          for (int c = 0; c < 3; ++c) input[c * plane + static_cast<size_t>(y) * bucket + x] = rgb[c] / 127.5f - 1.f;
        }
      }
    } else {
      int x0 = std::max(0, static_cast<int>(rect[0] * box.w));
      int y0 = std::max(0, static_cast<int>(rect[1] * box.h));
      int x1 = std::min(static_cast<int>(box.w), std::max(x0 + 1, static_cast<int>(rect[2] * box.w)));
      int y1 = std::min(static_cast<int>(box.h), std::max(y0 + 1, static_cast<int>(rect[3] * box.h)));
      int crop_width = x1 - x0, crop_height = y1 - y0;
      width = std::clamp(static_cast<int>(std::lround(48.0 * crop_width / crop_height)), 8, max_width);
      select_width();
      size_t plane = static_cast<size_t>(48) * bucket;
      input.resize(3 * plane);
      uint8_t rgb[3];
      for (int y = 0; y < 48; ++y) {
        float sy = std::clamp((y + .5f) * crop_height / 48.f - .5f, 0.f, crop_height - 1.f);
        for (int x = 0; x < width; ++x) {
          float sx = std::clamp((x + .5f) * crop_width / width - .5f, 0.f, crop_width - 1.f);
          nv12_rgb(frame, W, H, static_cast<int>(box.x) + x0 + static_cast<int>(std::lround(sx)),
                   static_cast<int>(box.y) + y0 + static_cast<int>(std::lround(sy)), rgb);
          for (int c = 0; c < 3; ++c) input[c * plane + static_cast<size_t>(y) * bucket + x] = rgb[c] / 127.5f - 1.f;
        }
      }
    }
#ifdef NP1_PLATE_GPU
    auto gpu_it = rec.gpu.find(bucket);
    if (gpu_it != rec.gpu.end()) {
      auto& model = *gpu_it->second;
      model.invoke([&](tflite::Interpreter* interpreter) {
        auto* in = interpreter->tensor(interpreter->inputs()[0]);
        size_t plane = static_cast<size_t>(48) * bucket;
        for (size_t i = 0; i < plane; ++i)
          for (int c = 0; c < 3; ++c) in->data.f[3 * i + c] = input[c * plane + i];
      });
      auto* interpreter = model.get();
      auto* out = interpreter->tensor(interpreter->outputs()[0]);
      return {out->data.f, std::min(out->dims->data[1], (width + 7) / 8), rec.dictionary.size(), Ort::Value{nullptr}};
    }
#endif
    const std::array<int64_t, 4> shape{1, 3, 48, bucket};
    auto memory = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    auto tensor = Ort::Value::CreateTensor<float>(memory, input.data(), input.size(), shape.data(), shape.size());
    const char* inputs[] = {rec.input_name.c_str()};
    const char* outputs[] = {rec.output_name.c_str()};
    const auto run_start = ProfileClock::now();
    std::vector<Ort::Value> result;
    try {
      result = rec.session->Run(Ort::RunOptions{nullptr}, inputs, &tensor, 1, outputs, 1);
    } catch (const Ort::Exception&) {
      failed_rec_ = &rec;
      throw;
    }
    {
#ifdef NP1_PLATE_MNN_GPU
      std::lock_guard<std::mutex> lk(prof_mu_);
#endif
      if (profile_) {
        profile_->input += std::chrono::duration<double, std::milli>(run_start - start).count();
        profile_->onnx += elapsed_ms(run_start);
        ++profile_->calls;
      }
    }
    auto info = result[0].GetTensorTypeAndShapeInfo();
    auto output_shape = info.GetShape();
    if (output_shape.size() != 3 || output_shape[0] != 1 || output_shape[1] < 0 ||
        output_shape[1] > std::numeric_limits<int>::max() || output_shape[2] != static_cast<int64_t>(rec.dictionary.size()))
      throw std::runtime_error("PP-OCR output shape does not match dictionary");
    int steps = static_cast<int>(output_shape[1]);
    if (!widths.empty()) steps = std::min(steps, (width + 7) / 8);
    const float* values = result[0].GetTensorData<float>();
    return {values, steps, rec.dictionary.size(), std::move(result[0])};
  }

  OCRText decode_probs(Recognizer& rec, const float* values, int steps) const {
    const auto& dictionary = rec.dictionary;
    OCRText decoded;
    int previous = -1;
    float maximum = 0;
    for (int t = 0; t <= steps; ++t) {
      int index = -1;
      float score = 0;
      if (t < steps) {
        index = 0;
        for (size_t i = 1; i < dictionary.size(); ++i)
          if (values[t * dictionary.size() + i] > values[t * dictionary.size() + index]) index = static_cast<int>(i);
        score = values[t * dictionary.size() + index];
      }
      if (index != previous) {
        if (previous > 0) {
          for (auto token : utf8_tokens(dictionary[previous])) {
            token = canonical_token(token);
            if (token != " " && token != "\t" && token != "\n") {
              decoded.tokens.push_back(token);
              decoded.scores.push_back(maximum);
            }
          }
        }
        previous = index;
        maximum = score;
      } else maximum = std::max(maximum, score);
    }
    return decoded;
  }

  std::string match_region_context_text(const OCRText& value, const std::string& classification) const {
    for (const auto& region : regions_) {
      auto region_tokens = utf8_tokens(region);
      if (value.tokens.size() < region_tokens.size() + classification.size()) continue;
      bool match = true;
      double log_probability = 0;
      for (size_t i = 0; i < region_tokens.size(); ++i) {
        if (canonical_token(value.tokens[i]) != region_tokens[i] || value.scores[i] < .85f) match = false;
        log_probability += std::log(std::max(1e-30f, value.scores[i]));
      }
      if (!match || std::exp(log_probability / region_tokens.size()) < .95) continue;
      for (size_t i = 0; i < classification.size(); ++i) {
        size_t at = region_tokens.size() + i;
        if (value.tokens[at] != classification.substr(i, 1) || value.scores[at] < .98f) match = false;
      }
      if (match) return region;
    }
    return {};
  }

#ifdef NP1_PLATE_MNN_GPU
  std::vector<float> make_fixed_input(const uint8_t* frame, int W, int H, const Box& box,
                                      const std::array<double, 4>& rect, int width) const {
    int x0 = std::max(0, static_cast<int>(rect[0] * box.w));
    int y0 = std::max(0, static_cast<int>(rect[1] * box.h));
    int x1 = std::min(static_cast<int>(box.w), std::max(x0 + 1, static_cast<int>(rect[2] * box.w)));
    int y1 = std::min(static_cast<int>(box.h), std::max(y0 + 1, static_cast<int>(rect[3] * box.h)));
    int crop_width = x1 - x0, crop_height = y1 - y0;
    int content_width = std::clamp(static_cast<int>(std::lround(48.0 * crop_width / crop_height)), 8, width);
    size_t plane = static_cast<size_t>(48) * width;
    std::vector<float> input(3 * plane, 0.0f);
    uint8_t rgb[3];
    for (int y = 0; y < 48; ++y) {
      float sy = std::clamp((y + .5f) * crop_height / 48.f - .5f, 0.f, crop_height - 1.f);
      for (int x = 0; x < content_width; ++x) {
        float sx = std::clamp((x + .5f) * crop_width / content_width - .5f, 0.f, crop_width - 1.f);
        nv12_rgb(frame, W, H, static_cast<int>(box.x) + x0 + static_cast<int>(std::lround(sx)),
                 static_cast<int>(box.y) + y0 + static_cast<int>(std::lround(sy)), rgb);
        for (int c = 0; c < 3; ++c) input[c * plane + static_cast<size_t>(y) * width + x] = rgb[c] / 127.5f - 1.f;
      }
    }
    return input;
  }

  std::vector<float> run_gpu_rects(const uint8_t* frame, int W, int H, const Box& box,
                                   const std::vector<std::array<double, 4>>& rects) {
    const auto t_in = ProfileClock::now();
    std::vector<std::vector<float>> batch_in;
    batch_in.reserve(rects.size());
    for (const auto& rect : rects) batch_in.push_back(make_fixed_input(frame, W, H, box, rect, gpu_batch_->width()));
    const auto t_run = ProfileClock::now();
    auto probs = gpu_batch_->run_batch(batch_in);
    const double run_ms = elapsed_ms(t_run);
    const double in_ms = std::chrono::duration<double, std::milli>(t_run - t_in).count();
    {
      std::lock_guard<std::mutex> lk(prof_mu_);
      if (profile_) {
        profile_->input += in_ms;
        profile_->onnx += run_ms;
        profile_->calls += static_cast<int>(rects.size());
      }
    }
    return probs;
  }

  LexMatch lex_gpu_batch(const uint8_t* frame, int W, int H, const Box& box,
                         const std::vector<std::array<double, 4>>& rects, const CtcLexicon& lexicon,
                         const std::string& known, double threshold) {
    auto probs = run_gpu_rects(frame, W, H, box, rects);
    const int steps = gpu_batch_->steps();
    const size_t stride = static_cast<size_t>(steps) * main_.dictionary.size();
    if (probs.size() < rects.size() * stride) throw std::runtime_error("MNN GPU batch output truncated");
    LexMatch best;
    std::set<std::string> strong;
    for (size_t i = 0; i < rects.size(); ++i) {
      LexMatch match = lexicon.score(probs.data() + i * stride, steps, static_cast<int>(main_.dictionary.size()));
      if (match.posterior < .9) continue;
      if (!known.empty() && match.text == known && match.likelihood >= std::max(.6, threshold)) return match;
      if (match.likelihood >= .6) strong.insert(match.text);
      if (known.empty() && strong.size() > 1) return {};
      if (match.likelihood > best.likelihood) best = match;
    }
    return strong.size() > 1 ? LexMatch{} : best;
  }

  std::string region_context_gpu(const uint8_t* frame, int W, int H, const Box& box,
                                 const std::string& classification,
                                 const std::vector<std::array<double, 4>>& rects) {
    if (rects.size() < 2) return {};
    auto probs = run_gpu_rects(frame, W, H, box, rects);
    const int steps = gpu_batch_->steps();
    const size_t stride = static_cast<size_t>(steps) * main_.dictionary.size();
    if (probs.size() < 2 * stride) throw std::runtime_error("MNN GPU context output truncated");
    std::string first = match_region_context_text(decode_probs(main_, probs.data(), steps), classification);
    if (first.empty()) return {};
    return first == match_region_context_text(decode_probs(main_, probs.data() + stride, steps), classification) ? first : "";
  }
#endif

  template <typename ReadKana, typename ReadClass>
  json refine_once(const uint8_t* frame, int W, int H, const Box& box,
                   const Choice& base_class, const Choice& base_kana, const Choice& base_serial,
                   ReadKana read_kana, ReadClass read_class, bool number_only) {
    static const std::vector<std::array<double, 4>> region_rects{{{.194, 0, .519, .325}}, {{.15, 0, .53, .37}}, {{.12, 0, .56, .42}}};

    std::string classification = base_class.confidence >= .6f ? base_class.text : "";
    std::string kana = base_kana.confidence >= .6f ? base_kana.text : "";
    std::string serial = base_serial.text;
    float serial_confidence = base_serial.confidence;

    auto resolve_number = [&]() {
      if (!number_check_) return;
      Candidate number = number_candidate(frame, W, H, box, {.195, .34, 1, 1});
      if (number.score >= number_score_) {
        if (number_override_ && serial_confidence < .6f && number.text.size() == base_serial.text.size()) {
          serial = number.text;
          serial_confidence = number_override_confidence_enabled_ ? std::min(number.score, static_cast<float>(number_override_confidence_)) : number.score;
        } else if (serial_confidence >= .6f && number.text != serial) {
          Candidate second = number_candidate(frame, W, H, box, {.215, .38, 1, .98});
          if (second.score >= number_score_ && second.text == number.text && number.text.size() == base_serial.text.size()) {
            serial = number.text;
            serial_confidence = std::min(number.score, second.score);
            if (number_override_confidence_enabled_) serial_confidence = std::min(serial_confidence, static_cast<float>(number_override_confidence_));
          } else serial_confidence = 0;
        }
      } else if (number.score >= number_veto_score_ && number.text != serial &&
                 number.text.size() == base_serial.text.size()) {
        serial_confidence = 0;
      }
    };

    if (number_only) {
      resolve_number();
      return {{"region", ""}, {"class", classification}, {"kana", kana}, {"number", serial}, {"confidence", serial_confidence}};
    }

    // Skip expensive region/kana/night-context passes only when serial confidence
    // remains below the configured recording floor (min_confidence_) after PP-OCR
    // serial recovery/verification.
    bool number_done = false;
    if (min_confidence_ > 0.0f && serial_confidence < min_confidence_) {
      resolve_number();
      number_done = true;
      if (serial_confidence < min_confidence_) {
        return {{"region", ""}, {"class", classification}, {"kana", kana}, {"number", serial}, {"confidence", serial_confidence}};
      }
    }

    LexMatch region_match;
    LexMatch kana_match;
#ifdef NP1_PLATE_MNN_GPU
    if (gpu_batch_) {
      // Run Adreno GPU (region batch-3) and Kryo CPU (number + kana) in parallel.
      auto region_future = std::async(std::launch::async, [&] {
        return lex_gpu_batch(frame, W, H, box, region_rects, region_lexicon_, "", region_threshold_);
      });
      if (!number_done) {
        resolve_number();
        number_done = true;
      }
      if (min_confidence_ <= 0.0f || serial_confidence >= min_confidence_) {
        kana_match = lex(main_, frame, W, H, box, kana_rects_, kana_lexicon_, kana, kana_threshold_);
      }
      region_match = region_future.get();
    } else
#endif
    {
      region_match = lex(main_, frame, W, H, box, region_rects, region_lexicon_, "", region_threshold_);
      kana_match = lex(main_, frame, W, H, box, kana_rects_, kana_lexicon_, kana, kana_threshold_);
      if (!number_done) {
        resolve_number();
        number_done = true;
      }
    }

    if (min_confidence_ > 0.0f && serial_confidence < min_confidence_) {
      return {{"region", ""}, {"class", classification}, {"kana", kana}, {"number", serial}, {"confidence", serial_confidence}};
    }

    std::string region = region_match.likelihood >= region_threshold_ ? region_match.text : "";
    if (kana_match.likelihood >= kana_threshold_) kana = kana_match.text;
    else if (!kana.empty() && kana_match.likelihood >= .6 && kana_match.text != kana) kana.clear();
    else if (kana.empty()) {
      auto trained = read_kana();
      if (trained.confidence >= .98f && trained.text == base_kana.text) kana = trained.text;
    }
    if (classification.empty()) {
      auto trained = read_class();
      if (trained.confidence >= .98f && trained.text == base_class.text) classification = trained.text;
    }
    if (!top_rects_.empty()) {
      auto top = top_read(main_, frame, W, H, box);
      if (!top.region.empty()) region = region.empty() || region == top.region ? top.region : "";
      if (top.class_override || (classification.empty() && top_class_fill_))
        if (!top.classification.empty()) classification = top.classification;
    }
    if (night_fallback_) {
      const bool need_reg_ctx = region.empty() && !classification.empty();
      const bool need_kana_ctx = kana.empty() && serial_confidence >= .6f;
#ifdef NP1_PLATE_MNN_GPU
      if (gpu_batch_ && need_reg_ctx && need_kana_ctx) {
        auto reg_future = std::async(std::launch::async, [&] {
          std::string r = region_context_gpu(frame, W, H, box, classification, {{{.14, .055, .92, .365}}, {{.08, 0, .94, .4}}});
          if (r.empty()) r = region_context_gpu(frame, W, H, box, classification, {{{.08, 0, .95, .48}}, {{.08, 0, .95, .55}}});
          return r;
        });
        kana = kana_context(frame, W, H, box, serial, {{{0, .34, 1, 1}}, {{.015, .40, .99, .98}}});
        region = reg_future.get();
      } else if (gpu_batch_ && need_reg_ctx) {
        region = region_context_gpu(frame, W, H, box, classification, {{{.14, .055, .92, .365}}, {{.08, 0, .94, .4}}});
        if (region.empty()) region = region_context_gpu(frame, W, H, box, classification, {{{.08, 0, .95, .48}}, {{.08, 0, .95, .55}}});
      } else
#endif
      {
        if (need_reg_ctx) {
          region = region_context(frame, W, H, box, classification, {{{.14, .055, .92, .365}}, {{.08, 0, .94, .4}}});
          if (region.empty()) region = region_context(frame, W, H, box, classification, {{{.08, 0, .95, .48}}, {{.08, 0, .95, .55}}});
        }
        if (need_kana_ctx)
          kana = kana_context(frame, W, H, box, serial, {{{0, .34, 1, 1}}, {{.015, .40, .99, .98}}});
      }
    }
    return {{"region", region}, {"class", classification}, {"kana", kana}, {"number", serial}, {"confidence", serial_confidence}};
  }

  OCRText text(Recognizer& rec, const uint8_t* frame, int W, int H, const Box& box, const std::array<double, 4>& rect) {
    auto values = infer(rec, frame, W, H, box, rect);
    return decode_probs(rec, values.values, values.steps);
  }

  LexMatch lex(Recognizer& rec, const uint8_t* frame, int W, int H, const Box& box,
               const std::vector<std::array<double, 4>>& rects, const CtcLexicon& lexicon,
               const std::string& known, double threshold) {
    LexMatch best;
    std::set<std::string> strong;
    for (const auto& rect : rects) {
      const auto result = infer(rec, frame, W, H, box, rect);
      LexMatch match = lexicon.score(result.values, result.steps, static_cast<int>(result.classes));
      if (match.posterior < .9) continue;
      if (!known.empty() && match.text == known && match.likelihood >= std::max(.6, threshold)) return match;
      if (match.likelihood >= .6) strong.insert(match.text);
      if (known.empty() && strong.size() > 1) return {};
      if (match.likelihood > best.likelihood) best = match;
    }
    return strong.size() > 1 ? LexMatch{} : best;
  }

  struct TopRead { std::string region, classification; bool class_override = false; };

  // Region and class digits from whole top-row views. Each view is greedily decoded into tokens with the
  // per-token maximum probability vector; the leading name characters are aligned to every region name
  // (substitution cost -log p, insertion / deletion cost top_gap_) and the costs are summed over the views.
  TopRead top_read(Recognizer& rec, const uint8_t* frame, int W, int H, const Box& box) {
    const size_t C = rec.dictionary.size();
    std::vector<double> total(region_names_.size(), 0.);
    std::vector<std::pair<std::string, double>> classes;
    int with_region = 0, views = 0;
    for (const auto& rect : top_rects_) {
      ++views;
      auto values = infer(rec, frame, W, H, box, rect);
      std::vector<std::string> tokens;
      std::vector<float> scores;
      std::vector<std::vector<float>> vectors;
      size_t previous = 0;
      for (int t = 0; t < values.steps; ++t) {
        const float* row = values.values + static_cast<size_t>(t) * C;
        size_t k = std::max_element(row, row + C) - row;
        if (k != 0 && k != previous) {
          auto parts = utf8_tokens(rec.dictionary[k]);
          tokens.push_back(parts.size() == 1 ? canonical_token(parts[0]) : rec.dictionary[k]);
          scores.push_back(row[k]);
          vectors.emplace_back(row, row + C);
        } else if (k != 0 && !scores.empty()) {
          scores.back() = std::max(scores.back(), row[k]);
          for (size_t i = 0; i < C; ++i) vectors.back()[i] = std::max(vectors.back()[i], row[i]);
        }
        previous = k;
      }
      std::vector<size_t> name;
      std::string klass;
      float class_minimum = 1;
      for (size_t j = 0; j < tokens.size(); ++j) {
        if (is_class_token(tokens[j]) && !name.empty()) {
          klass += tokens[j];
          class_minimum = std::min(class_minimum, scores[j]);
          if (klass.size() == 3) break;
        } else if (klass.empty() && (region_chars_.count(tokens[j]) || is_name_token(tokens[j]))) {
          name.push_back(j);
        }
      }
      if (!name.empty()) {
        ++with_region;
        for (size_t r = 0; r < region_names_.size(); ++r) total[r] += align_cost(vectors, name, region_names_[r].second);
      }
      if (valid_classification(klass)) {
        auto entry = std::find_if(classes.begin(), classes.end(), [&](const auto& item) { return item.first == klass; });
        if (entry == classes.end()) classes.emplace_back(klass, class_minimum);
        else entry->second += class_minimum;
      }
    }
    TopRead out;
    if (with_region > 0 && 2 * with_region >= views && !region_names_.empty()) {
      size_t best = std::min_element(total.begin(), total.end()) - total.begin();
      double normalizer = 0;
      for (double v : total) normalizer += std::exp(-(v - total[best]));
      double posterior = 1. / normalizer;
      size_t length = region_names_[best].second.size();
      if (total[best] / with_region <= top_max_cost_ * length && posterior >= top_posterior_) out.region = region_names_[best].first;
    }
    if (!classes.empty()) {
      auto best = std::max_element(classes.begin(), classes.end(), [](const auto& a, const auto& b) { return a.second < b.second; });
      double sum = 0;
      for (const auto& entry : classes) sum += entry.second;
      out.class_override = top_class_override_enabled_ && classes.size() == 1 && best->second / views >= top_class_override_;
      if (out.class_override || (sum > 0 && best->second / views >= top_class_minimum_ && best->second / sum >= top_class_share_))
        out.classification = best->first;
    }
    return out;
  }

  double align_cost(const std::vector<std::vector<float>>& vectors, const std::vector<size_t>& name,
                    const std::vector<std::string>& target) const {
    const size_t n = name.size(), m = target.size();
    std::vector<double> previous(m + 1), current(m + 1);
    for (size_t j = 0; j <= m; ++j) previous[j] = j * top_gap_;
    for (size_t i = 1; i <= n; ++i) {
      current[0] = i * top_gap_;
      const auto& vector = vectors[name[i - 1]];
      for (size_t j = 1; j <= m; ++j) {
        double substitution = 20.;
        auto found = token_ids_.find(target[j - 1]);
        if (found != token_ids_.end()) {
          double p = 0;
          for (int id : found->second) p += vector[id];
          substitution = -std::log(std::max(1e-30, p));
        }
        current[j] = std::min({previous[j - 1] + substitution, previous[j] + top_gap_, current[j - 1] + top_gap_});
      }
      previous.swap(current);
    }
    return previous[m];
  }

  static bool serial_text(const std::string& value, const std::string& expected) {
    if (!valid_serial(expected)) return false;
    std::string digits;
    for (char c : value) if (c >= '0' && c <= '9') digits.push_back(c);
    if (digits != expected) return false;
    return std::all_of(value.begin(), value.end(), [](unsigned char c) {
      return (c >= '0' && c <= '9') || c == '-' || c == '.' || c >= 0x80;
    });
  }

  std::string region_context(const uint8_t* frame, int W, int H, const Box& box,
                             const std::string& classification,
                             const std::vector<std::array<double, 4>>& rects) {
    auto decode = [&](const std::array<double, 4>& rect) {
      return match_region_context_text(text(main_, frame, W, H, box, rect), classification);
    };
    if (rects.size() < 2) return {};
    std::string first = decode(rects[0]);
    return !first.empty() && first == decode(rects[1]) ? first : "";
  }

  std::string kana_context(const uint8_t* frame, int W, int H, const Box& box, const std::string& serial,
                           const std::vector<std::array<double, 4>>& rects) {
    auto decode = [&](const std::array<double, 4>& rect) {
      OCRText value = text(main_, frame, W, H, box, rect);
      if (value.tokens.empty()) return std::string{};
      std::string first = canonical_token(value.tokens[0], true);
      if (!kana_.count(first) || value.scores[0] < .95f || !serial_text(join_tokens(value.tokens, 1), serial)) return std::string{};
      for (size_t i = 1; i < value.tokens.size(); ++i)
        if (is_digit_token(value.tokens[i]) && value.scores[i] < .98f) return std::string{};
      return first;
    };
    if (rects.size() < 2) return {};
    std::string first = decode(rects[0]);
    return !first.empty() && first == decode(rects[1]) ? first : "";
  }

  Candidate number_candidate(const uint8_t* frame, int W, int H, const Box& box,
                             const std::array<double, 4>& rect) {
    OCRText value = text(number_ ? *number_ : main_, frame, W, H, box, rect);
    Candidate result;
    result.score = 1;
    for (size_t i = 0; i < value.tokens.size(); ++i) {
      if (is_digit_token(value.tokens[i])) {
        result.text += value.tokens[i];
        result.score = std::min(result.score, value.scores[i]);
      } else if (!is_dot_token(value.tokens[i]) && !is_hyphen_token(value.tokens[i])) return {};
    }
    return valid_serial(result.text) ? result : Candidate{};
  }

  std::unique_ptr<Ort::Env> env_;
  Recognizer main_;
  std::unique_ptr<Recognizer> number_;
  Recognizer* failed_rec_ = nullptr;
  const int threads_;
  const float min_confidence_;
  std::set<std::string> regions_, kana_;
  CtcLexicon region_lexicon_, kana_lexicon_;
  double region_threshold_ = .6, kana_threshold_ = .6;
  bool night_fallback_ = false;
  FrameProfile* profile_ = nullptr;
#ifdef NP1_PLATE_MNN_GPU
  std::unique_ptr<MnnGpuBatch> gpu_batch_;
  std::mutex prof_mu_;
#endif
  bool number_check_ = true, number_override_ = true, number_override_confidence_enabled_ = false;
  double number_score_ = static_cast<double>(.98f), number_veto_score_ = static_cast<double>(.98f);
  double number_override_confidence_ = 1.0;
  std::vector<Rect> kana_rects_, top_rects_;
  double top_posterior_ = .9, top_gap_ = 4., top_max_cost_ = 4., top_class_minimum_ = .6, top_class_share_ = .8;
  bool top_class_fill_ = true;
  double top_class_override_ = 0.;
  bool top_class_override_enabled_ = false;
  std::vector<std::pair<std::string, std::vector<std::string>>> region_names_;
  std::set<std::string> region_chars_;
  std::map<std::string, std::vector<int>> token_ids_;
};

float iou(const Box& a, const Box& b) {
  float x1 = std::max(a.x, b.x), y1 = std::max(a.y, b.y);
  float x2 = std::min(a.x + a.w, b.x + b.w), y2 = std::min(a.y + a.h, b.y + b.h);
  float inter = std::max(0.f, x2 - x1) * std::max(0.f, y2 - y1);
  return inter / (a.w * a.h + b.w * b.h - inter + 1e-6f);
}

#include "numeric_verifier.h"

class Engine {
 public:
  Engine(const std::string& det, const std::string& ocr, const Config& cfg, int threads,
         GPU detector_gpu, bool use_gpu, const std::string& gpu_cache, bool prepare_cache = false,
         float min_confidence = 0.3f)
      : cfg_(cfg), threads_(threads), det_(det, threads, detector_gpu, gpu_cache, prepare_cache),
        ocr_(ocr, threads, use_gpu ? GPU::full_graph : GPU::off, gpu_cache, prepare_cache) {
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
    if (!cfg_.numeric_verifier.empty()) {
      if (ow_ != 192 || oh_ != 96 || oc_ != 1) throw std::runtime_error("numeric verifier requires the grayscale reader");
      const std::vector<std::string> digits{"", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"};
      size_t number_heads = 0;
      for (const auto& head : cfg_.heads) {
        if (head.field != "number") continue;
        if (head.labels != digits) throw std::runtime_error("numeric verifier requires the standard digit labels");
        ++number_heads;
      }
      if (number_heads != 4) throw std::runtime_error("numeric verifier requires four digit heads");
      const auto file = std::filesystem::path(cfg_.model_dir) / cfg_.numeric_verifier.at("file").get<std::string>();
      const bool accelerate = use_gpu && cfg_.numeric_verifier.value("accelerate", false);
      const GPU verifier_gpu = accelerate ? (prepare_cache ? GPU::full_graph : GPU::cached_full_graph) : GPU::off;
      numeric_verifier_ = std::make_unique<NumericVerifier>(file.string(), cfg_.numeric_verifier, threads,
                                                           verifier_gpu, gpu_cache, prepare_cache);
    }
    size_t total = 0;
    for (auto& h : cfg_.heads) total += h.labels.size();
    auto* oout = ocr_->tensor(ocr_->outputs()[0]);
    size_t n = 1;
    for (int i = 0; i < oout->dims->size; ++i) n *= oout->dims->data[i];
    if (n != total) throw std::runtime_error("reader output size does not match configured heads");

    std::vector<Head> kana_heads, class_heads;
    std::vector<std::string> regions, kana;
    for (const auto& head : cfg_.heads) {
      if (head.field == "region") regions = head.labels;
      else if (head.field == "kana") { kana = head.labels; kana_heads.push_back(head); }
      else if (head.field == "class") class_heads.push_back(head);
    }
    if (cfg_.field_readers.contains("kana")) {
      auto spec = cfg_.field_readers.at("kana");
      aux_kana_ = std::make_unique<AuxReader>((std::filesystem::path(cfg_.model_dir) / spec.at("file").get<std::string>()).string(),
                                               spec, kana_heads, threads, use_gpu, gpu_cache, prepare_cache);
    }
    if (cfg_.field_readers.contains("classification")) {
      auto spec = cfg_.field_readers.at("classification");
      aux_class_ = std::make_unique<AuxReader>((std::filesystem::path(cfg_.model_dir) / spec.at("file").get<std::string>()).string(),
                                                 spec, class_heads, threads, use_gpu, gpu_cache, prepare_cache);
    }
    if (!cfg_.text_ocr.is_null() && !cfg_.text_ocr.empty()) {
      auto model = (std::filesystem::path(cfg_.model_dir) / cfg_.text_ocr.at("file").get<std::string>()).string();
      auto dictionary = (std::filesystem::path(cfg_.model_dir) / cfg_.text_ocr.at("dictionary").get<std::string>()).string();
      auto fast = cfg_.text_ocr.value("fastFile", std::string{});
      const char* ppocr = std::getenv("NCC_PLATE_PPOCR");
      if (ppocr && std::strcmp(ppocr, "original") == 0) fast.clear();
      if (!fast.empty()) fast = (std::filesystem::path(cfg_.model_dir) / fast).string();
      text_ocr_ = std::make_unique<TextOCR>(model, fast, dictionary, cfg_.model_dir, cfg_.text_ocr, regions, kana,
                                            use_gpu, gpu_cache, prepare_cache, min_confidence);
    }
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
    double det = time_ms([&] { detect(frame.data(), 1920, 1080, 1); });
    double lite = time_ms([&] { read(frame.data(), 1920, 1080, b, false); });
    double ocr = time_ms([&] {
      json evidence;
      read(frame.data(), 1920, 1080, b, true, nullptr, &evidence);
    });
    return {{"detectorMs", det}, {"readerTFLiteMs", lite}, {"readerMs", ocr}, {"runs", n}};
  }

  json process(const uint8_t* f, int W, int H, uint64_t sequence, FrameProfile* profile) {
    json plates = json::array();
    const auto start = ProfileClock::now();
    const auto boxes = detect(f, W, H, sequence);
    if (profile) profile->detection = elapsed_ms(start);
    const bool number_only_interval = sequence > 0 && (sequence - 1) % cfg_.full_ocr_every != 0;
    bool checked_overlay = false;
    bool has_overlay = false;
    std::vector<std::pair<size_t, const Box*>> crops, small_crops;
    for (const Box& b : boxes) {
      if (b.w < cfg_.min_width) continue;
      if (cfg_.ignore_recorder_overlay && in_recorder_overlay(b, W, H)) {
        if (!checked_overlay) {
          has_overlay = recorder_overlay(f, W, H);
          checked_overlay = true;
        }
        if (has_overlay) continue;
      }
      if (profile) ++profile->candidates;
      const auto width = std::lround(b.w);
      json observation = {{"box", {std::lround(b.x), std::lround(b.y), width, std::lround(b.h)}},
                          {"score", b.score}, {"reading", json::object()}};
      // A weak or distant detection can extend a track before it is suitable for OCR.
      if (width >= cfg_.ocr_min_crop_width && b.score >= cfg_.ocr_min_detection_score)
        crops.emplace_back(plates.size(), &b);
      else if (cfg_.reread_shrinking_digits && shrinking_crop_ && width >= 32 && width <= 34 &&
               b.score >= std::max(.50f, cfg_.ocr_min_detection_score))
        small_crops.emplace_back(plates.size(), &b);
      plates.push_back(std::move(observation));
    }
    std::vector<RecentCrop> current_crops;
    if (cfg_.crowded_ocr_interval > 1) current_crops.reserve(crops.size());
    bool handled_reread = false;
    std::optional<Box> warm_candidate;
    for (const auto& [index, box] : crops) {
      bool deferred = false;
      if (cfg_.crowded_ocr_interval > 1) {
        RecentCrop current{*box, 1, 0};
        const RecentCrop* previous = nullptr;
        constexpr float overlap = .62f;
        constexpr int initial_reads = 3;
        for (const auto& crop : recent_ocr_crops_) {
          if (iou(crop.box, *box) < overlap) continue;
          if (previous) {
            previous = nullptr;
            break;
          }
          previous = &crop;
        }
        // Leave new, growing and ambiguous crops eligible for immediate OCR.
        // Keep the detector geometry even when the crop is read less often.
        if (previous && box->w < 1.18f * previous->box.w &&
            std::count_if(crops.begin(), crops.end(), [&](const auto& crop) {
              return iou(*crop.second, previous->box) >= overlap;
            }) == 1) {
          current.consecutive = std::min(previous->consecutive + 1, initial_reads);
          if (crops.size() > 1 && previous->consecutive >= initial_reads &&
              previous->deferred < cfg_.crowded_ocr_interval - 1) {
            current.deferred = previous->deferred + 1;
          }
        }
        current_crops.push_back(current);
        deferred = current.deferred != 0;
      }
      json evidence;
      const bool number_only = deferred || number_only_interval || std::lround(box->w) < cfg_.full_ocr_min_width;
      auto& observation = plates[index];
      if (!deferred) {
        observation["reading"] = read(f, W, H, *box, true, profile, &evidence, number_only);
      }
      const bool may_reread = (cfg_.reread_narrow_cool_crops || cfg_.reread_crowded_warm_crops) &&
                              numeric_verifier_ && !handled_reread && (deferred || !evidence.empty());
      const int tone = may_reread ? narrow_crop_tone(f, W, H, *box) : 0;
      constexpr int tone_threshold = 15 * 25;
      const bool warm = cfg_.reread_crowded_warm_crops && tone <= -tone_threshold &&
                        (crops.size() == 1 || !previous_warm_candidate_ ||
                         iou(*previous_warm_candidate_, *box) < .62f);
      if (warm || (cfg_.reread_narrow_cool_crops && tone >= tone_threshold)) {
        const double strength = matching_serial_strength(observation["reading"], evidence);
        if (strength < .99) {
          handled_reread = true;
          // Remember the lone warm candidate so it yields the first reread
          // when another plate enters the frame.
          if (warm) warm_candidate = *box;
          if (!warm || crops.size() > 1) {
            json revised_evidence;
            json revised;
            if (warm && std::lround(box->w) >= 45) {
              const Box inset{std::round(box->x) + 1, std::round(box->y) + 1,
                              std::round(box->w) - 2, std::round(box->h) - 2, box->score};
              revised = read(f, W, H, inset, true, profile, &revised_evidence, number_only);
            } else {
              revised = reread_contrast_crop(f, W, H, *box, profile, number_only, &revised_evidence);
            }
            if (matching_serial_strength(revised, revised_evidence) >= std::max(.65, strength + .10)) {
              observation["reading"] = std::move(revised);
              evidence = std::move(revised_evidence);
            }
          }
        }
      }
      if (!evidence.empty()) observation.update(evidence);
    }
    if (cfg_.reread_shrinking_digits && numeric_verifier_)
      read_shrinking_plate(f, W, H, sequence, profile, crops, small_crops, plates);
    if (cfg_.require_digit_detail && numeric_verifier_)
      for (auto& observation : plates) measure_digit_detail(f, W, H, observation);
    recent_ocr_crops_ = std::move(current_crops);
    previous_warm_candidate_ = warm_candidate;
    return plates;
  }

  void prepare_gpu_cache() {
    std::vector<uint8_t> frame(1920 * 1080 * 3 / 2, 128);
    detect(frame.data(), 1920, 1080, 1);
    det_.mark_cache_ready(true);
    Box b{800, 600, 200, 100, 1};
    read(frame.data(), 1920, 1080, b, false);
    ocr_.mark_cache_ready(false);
    if (aux_kana_) aux_kana_->mark_cache_ready();
    if (aux_class_) aux_class_->mark_cache_ready();
    if (numeric_verifier_) numeric_verifier_->prepare_gpu_cache();
    if (text_ocr_) text_ocr_->mark_gpu_cache_ready();
  }

  bool gpu_active() const { return text_ocr_ && text_ocr_->gpu_active(); }
  bool latest_models() const { return aux_kana_ && aux_class_ && text_ocr_; }

  std::string backend() const {
    std::string name = "xnnpack cpu x" + std::to_string(threads_);
    if (det_.uses_gpu()) name += " + gpu detector";
    if (ocr_.uses_gpu()) name += " + gpu ocr";
    if (aux_kana_ && aux_kana_->uses_gpu()) name += " + gpu kana";
    if (aux_class_ && aux_class_->uses_gpu()) name += " + gpu class";
    if (numeric_verifier_ && numeric_verifier_->uses_gpu()) name += " + gpu numeric verification";
    if (text_ocr_) {
      if (text_ocr_->uses_gpu()) name += " + gpu ppocr";
      name += text_ocr_->uses_int8() ? " + onnx int8 cpu" : " + onnx cpu";
    }
    return name;
  }

 private:
  static bool in_recorder_overlay(const Box& box, int W, int H) {
    const float x = (box.x + box.w / 2) / W, y = (box.y + box.h / 2) / H;
    return x >= .60f && x <= .96f && y >= .82f && y <= .92f;
  }

  static bool recorder_overlay(const uint8_t* f, int W, int H) {
    // The recorder writes two yellow lines spanning the lower right. Check both
    // bands so a plate in that part of an unstamped frame stays eligible.
    const auto has_yellow_band = [&](float top, float bottom) {
      std::array<int, 6> occupied{};
      const int left = static_cast<int>(.625f * W), right = static_cast<int>(.948f * W);
      int count = 0, total = 0;
      uint8_t rgb[3];
      for (int y = static_cast<int>(top * H); y < static_cast<int>(bottom * H); y += 2) {
        for (int x = left; x < right; x += 2) {
          nv12_rgb(f, W, H, x, y, rgb);
          ++total;
          const bool yellow = rgb[0] >= 150 && rgb[1] >= 150 &&
              std::min(rgb[0], rgb[1]) - rgb[2] >= 45 && std::abs(rgb[0] - rgb[1]) <= 75;
          if (yellow) {
            ++count;
            ++occupied[6 * (x - left) / (right - left)];
          }
        }
      }
      return total > 0 && count >= .06f * total && count <= .35f * total &&
             std::count_if(occupied.begin(), occupied.end(), [&](int n) { return n >= .006f * total; }) >= 4;
    };
    return has_yellow_band(.824f, .867f) && has_yellow_band(.874f, .916f);
  }

  std::vector<Box> detect(const uint8_t* f, int W, int H, uint64_t sequence) {
    auto boxes = detect_roi(f, W, H, cfg_.roi);
    if (!cfg_.side_rois.empty()) {
      const auto index = sequence > 0 ? (sequence - 1) % cfg_.side_rois.size() : 0;
      const auto side = detect_roi(f, W, H, cfg_.side_rois[index]);
      boxes.insert(boxes.end(), side.begin(), side.end());
      boxes = suppress_overlaps(std::move(boxes));
    }
    if (cfg_.fallback_roi && std::none_of(boxes.begin(), boxes.end(), [&](const Box& box) {
          return box.score >= .40f && std::lround(box.w) >= std::max(35, cfg_.min_width);
        })) {
      const auto extra = detect_roi(f, W, H, *cfg_.fallback_roi);
      boxes.insert(boxes.end(), extra.begin(), extra.end());
      boxes = suppress_overlaps(std::move(boxes));
    }
    return boxes;
  }

  std::vector<Box> detect_roi(const uint8_t* f, int W, int H, const std::array<int, 4>& roi) {
    int rx = std::clamp(roi[0], 0, W - 2), ry = std::clamp(roi[1], 0, H - 2);
    int rw = std::clamp(roi[2], 2, W - rx), rh = std::clamp(roi[3], 2, H - ry);
    float s = std::min(static_cast<float>(dw_) / rw, static_cast<float>(dh_) / rh);
    float px = (dw_ - rw * s) / 2, py = (dh_ - rh * s) / 2;
    det_.invoke([&](auto* interpreter) {
      TfLiteTensor* t = interpreter->tensor(interpreter->inputs()[0]);
#if defined(__aarch64__)
      if ((rx & 1) == 0 && rw == dw_ * 2 && rh == dh_ * 2 && (dw_ % 8) == 0) {
        if (t->type == kTfLiteFloat32) {
          fill_det_neon_half_f32(f, W, H, rx, ry, dw_, dh_, t->data.f);
          return;
        }
        if (t->type == kTfLiteInt8 && std::fabs(t->params.scale - (1.f / 255.f)) < 1e-6f && t->params.zero_point == -128) {
          fill_det_neon_half_int8(f, W, H, rx, ry, dw_, dh_, t->data.int8);
          return;
        }
      }
#endif
      InputWriter in{t};
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
    });
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
    return suppress_overlaps(std::move(cand));
  }

  std::vector<Box> suppress_overlaps(std::vector<Box> cand) const {
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


  static void measure_digit_detail(const uint8_t* frame, int width, int height, json& observation) {
    if (!observation.contains("numberScores")) return;
    const auto [x, y, w, h] = observation.at("box").get<std::array<int, 4>>();
    if (w < 32 || w > 55 || h < 12 || x < 0 || y < 0 || x + w > width || y + h > height) return;
    const auto supports_four_digits = [&](const char* key) {
      const auto reading = observation.find(key);
      return reading != observation.end() && reading->is_object() && reading->value("number", std::string{}).size() == 4 &&
             reading->value("confidence", 0.) >= .60;
    };
    if (!supports_four_digits("reading") && !supports_four_digits("verifier")) return;

    const int left = x + 1, right = x + w - 1;
    const int top = y + static_cast<int>(std::nearbyint(h * .33)), bottom = y + h - 1;
    int horizontal = 0, vertical = 0;
    for (int row = top; row < bottom; ++row) {
      const uint8_t* line = frame + row * width;
      for (int col = left; col < right; ++col) {
        constexpr int minimum_luma_step = 16;
        if (col + 1 < right && std::abs(int(line[col + 1]) - line[col]) >= minimum_luma_step) ++horizontal;
        if (row + 1 < bottom && std::abs(int(line[col + width]) - line[col]) >= minimum_luma_step) ++vertical;
      }
    }
    const double across = horizontal / double((bottom - top) * (right - left - 1));
    const double down = vertical / double((bottom - top - 1) * (right - left));
    observation["digitEdges"] = (across + down) / 2;
  }

  static bool confirms_digits(const json& reading, const json& evidence, const std::string& serial,
                              double primary, double verifier, double localized, double ctc) {
    const auto sees = [&serial](const json& r, double minimum) {
      return r.is_object() && r.value("number", std::string{}) == serial && r.value("confidence", 0.) >= minimum;
    };
    const auto confirms = [&](const char* key, double minimum) {
      const auto check = evidence.find(key);
      return check != evidence.end() && sees(*check, minimum);
    };
    const auto scores = evidence.find("numberScores");
    return sees(reading, primary) && confirms("verifier", verifier) && confirms("localized", localized) &&
           evidence.value("ctc", std::string{}) == serial && scores != evidence.end() && scores->value(serial, 0.) >= ctc;
  }

  void read_shrinking_plate(const uint8_t* frame, int w, int h, uint64_t sequence, FrameProfile* profile,
                            const std::vector<std::pair<size_t, const Box*>>& regular,
                            const std::vector<std::pair<size_t, const Box*>>& small, json& plates) {
    const auto prior = std::exchange(shrinking_crop_, std::nullopt);
    const auto x = [](const Box& b) { return b.x + b.w/2; };
    const auto y = [](const Box& b) { return b.y + b.h/2; };
    if (prior && prior->sequence + 1 == sequence && prior->remaining > 0) {
      const auto& last = prior->last;
      const float side = x(last) - w/2.f;
      const auto follows = [&](const auto& crop) {
        const auto& b = *crop.second;
        const auto dx = x(b)-x(last);
        return side*dx < 0 && std::abs(dx) >= .25f*last.w && std::abs(dx) <= 1.35f*last.w &&
               std::abs(y(b)-y(last)) <= .4f*last.h;
      };
      const auto found = std::find_if(small.begin(), small.end(), follows);
      if (found != small.end() && std::none_of(std::next(found), small.end(), follows)) {
        const auto& [index, box] = *found;
        json evidence = json::object();
        auto reading = read(frame, w, h, *box, true, profile, &evidence, true);
        if (confirms_digits(reading, evidence, prior->serial, .75, .70, .87, .96)) {
          plates[index]["reading"] = std::move(reading);
          plates[index].update(evidence);
          if (prior->remaining > 1)
            shrinking_crop_ = ShrinkingCrop{*box, prior->serial, sequence, prior->remaining-1};
          return;
        }
      }
    }
    for (const auto& [index, box] : regular) {
      const int width = std::lround(box->w);
      if (width < 35 || width > 44 || box->score < .58f || std::abs(x(*box)-w/2.f) < .14f*w) continue;
      const auto& candidate = plates[index];
      const auto& reading = candidate.at("reading");
      const auto serial = reading.value("number", std::string{});
      if (serial.size() != 4 || !confirms_digits(reading, candidate, serial, .80, .73, .85, .97)) continue;
      if (shrinking_crop_) {
        shrinking_crop_.reset();
        return;
      }
      shrinking_crop_ = ShrinkingCrop{*box, serial, sequence, 2};
    }
  }

  static double matching_serial_strength(const json& reading, const json& evidence) {
    const auto number = reading.value("number", std::string{});
    const auto verifier = evidence.find("verifier");
    if (number.empty() || verifier == evidence.end() || !verifier->is_object() ||
        verifier->value("number", std::string{}) != number) return 0.;
    const auto scores = evidence.find("numberScores");
    const double score = scores == evidence.end() ? 0. : scores->value(number, 0.);
    const auto localized = evidence.find("localized");
    const bool local_agreement = localized != evidence.end() && localized->is_object() &&
                                 localized->value("number", std::string{}) == number &&
                                 localized->value("confidence", 0.) >= .78;
    return std::min(reading.value("confidence", 0.), verifier->value("confidence", 0.)) +
           .25 * score + .10 * (evidence.value("ctc", std::string{}) == number) + .10 * local_agreement;
  }

  static int narrow_crop_tone(const uint8_t* frame, int width, int height, const Box& box) {
    const int x = std::lround(box.x), y = std::lround(box.y);
    const int w = std::lround(box.w), h = std::lround(box.h);
    if (box.score < .31f || w < 39 || w > 55 || h < 18 || h > 38 || x < 32 || y < 32 ||
        x + w + 32 >= width || y + h + 32 >= height) return 0;
    const uint8_t* uv = frame + width * height;
    int sum = 0;
    for (int row = 0; row < 5; ++row) {
      const uint8_t* line = uv + (y + (2 * row + 1) * h / 10) / 2 * width;
      for (int col = 0; col < 5; ++col) {
        const int at = (x + (2 * col + 1) * w / 10) & ~1;
        sum += static_cast<int>(line[at]) - line[at + 1];
      }
    }
    return sum;
  }

  json reread_contrast_crop(const uint8_t* frame, int width, int height, const Box& box,
                           FrameProfile* profile, bool number_only, json* evidence) {
    const int x = std::lround(box.x), y = std::lround(box.y);
    const int w = std::lround(box.w), h = std::lround(box.h);
    // Copy only the local NV12 window. Even origins preserve the UV sample alignment.
    const int left = (x - 24) & ~1, top = (y - 24) & ~1;
    const int cols = ((x + w + 25) & ~1) - left;
    const int rows = ((y + h + 25) & ~1) - top;
    std::vector<uint8_t> patch(static_cast<size_t>(cols) * rows * 3 / 2);
    for (int row = 0; row < rows; ++row)
      std::copy_n(frame + (top + row) * width + left, cols, patch.data() + row * cols);
    for (int row = 0; row < rows / 2; ++row)
      std::copy_n(frame + width * height + (top / 2 + row) * width + left, cols,
                  patch.data() + rows * cols + row * cols);

    const int x0 = ((x - 5) & ~1) - left, y0 = ((y - 5) & ~1) - top;
    const int x1 = ((x + w + 7) & ~1) - left, y1 = ((y + h + 7) & ~1) - top;
    constexpr int radius = 6;
    constexpr int filter = 2 * radius + 1;
    const int pitch = x1 - x0 + 2 * radius + 1;
    const int integral_rows = y1 - y0 + 2 * radius + 1;
    std::vector<int> integral(static_cast<size_t>(pitch) * integral_rows);
    for (int row = 1; row < integral_rows; ++row) {
      const int sy = std::clamp(y0 + row - radius - 1, y0, y1 - 1);
      int sum = 0;
      for (int col = 1; col < pitch; ++col) {
        const int sx = std::clamp(x0 + col - radius - 1, x0, x1 - 1);
        sum += patch[sy * cols + sx];
        integral[row * pitch + col] = integral[(row - 1) * pitch + col] + sum;
      }
    }
    // Reduce changes in the local background while retaining the contrast of digit strokes.
    for (int row = y0; row < y1; ++row) {
      const int upper = (row - y0) * pitch, lower = upper + filter * pitch;
      for (int col = x0; col < x1; ++col) {
        const int offset = col - x0;
        const int sum = integral[lower + offset + filter] - integral[upper + offset + filter] -
                        integral[lower + offset] + integral[upper + offset];
        auto& pixel = patch[row * cols + col];
        const double background = sum / double(filter * filter);
        pixel = static_cast<uint8_t>(std::clamp(.64 * pixel + .36 * 155 + 1.12 * (pixel - background), 16., 235.));
      }
    }
    for (int row = y0 / 2; row < y1 / 2; ++row)
      std::fill_n(patch.data() + rows * cols + row * cols + x0, x1 - x0, 128);

    const Box crop{box.x - left, box.y - top, box.w, box.h, box.score};
    return read(patch.data(), cols, rows, crop, true, profile, evidence, number_only);
  }

  std::vector<uint8_t> expanded_digit_crop(const uint8_t* f, int width, int height, const Box& box) const {
    const float w = std::round(box.w) + 4.f, h = std::round(box.h) + 4.f;
    const float left = std::round(box.x) - 2.f - w * cfg_.margin;
    const float top = std::round(box.y) - 1.f - h * cfg_.margin;
    const float dx = w * (1.f + 2.f * cfg_.margin) / ow_;
    const float dy = h * (1.f + 2.f * cfg_.margin) / oh_;
    std::vector<uint8_t> pixels(static_cast<size_t>(oh_) * ow_);
    sample_gray_crop(f, width, height, ow_, oh_, [&](int x) { return left + (x + .5f) * dx; },
                     [&](int y) { return top + (y + .5f) * dy; },
                     [&](size_t at, uint8_t level) { pixels[at] = level; });
    return pixels;
  }

  json read(const uint8_t* f, int W, int H, const Box& b, bool refine = true,
            FrameProfile* profile = nullptr, json* evidence = nullptr, bool number_only = false) {
    const auto start = ProfileClock::now();
    float mx = b.w * cfg_.margin, my = b.h * cfg_.margin;
    float x0 = b.x - mx, y0 = b.y - my, cw = b.w + 2 * mx, ch = b.h + 2 * my;
    std::vector<uint8_t> crop;
    if (evidence && numeric_verifier_) crop.resize(static_cast<size_t>(oh_) * ow_);
    ocr_.invoke([&](auto* interpreter) {
      InputWriter in{interpreter->tensor(interpreter->inputs()[0])};
      const auto at_x = [&](int x) { return x0 + (x + .5f) * cw / ow_; };
      const auto at_y = [&](int y) { return y0 + (y + .5f) * ch / oh_; };
      if (oc_ == 1) {
        sample_gray_crop(f, W, H, ow_, oh_, at_x, at_y, [&](size_t at, uint8_t level) {
          in.set(at, level / 255.f);
          if (!crop.empty()) crop[at] = level;
        });
        return;
      }
      uint8_t rgb[3];
      for (int y = 0; y < oh_; ++y) {
        for (int x = 0; x < ow_; ++x) {
          const size_t i = (static_cast<size_t>(y) * ow_ + x) * oc_;
          nv12_rgb(f, W, H, static_cast<int>(at_x(x)), static_cast<int>(at_y(y)), rgb);
          for (int c = 0; c < 3; ++c) in.set(i + c, rgb[c] / 255.f);
        }
      }
    });
    const TfLiteTensor* out = ocr_->tensor(ocr_->outputs()[0]);
    std::vector<float> region_probability, kana_probability;
    std::vector<std::string> region_labels, kana_labels;
    std::vector<std::vector<float>> class_probability, number_probability;
    std::vector<std::vector<std::string>> class_labels, number_labels;
    size_t offset = 0;
    for (const auto& h : cfg_.heads) {
      std::vector<float> probabilities = softmax(out, offset, h.labels.size());
      if (h.field == "region") { region_probability = std::move(probabilities); region_labels = h.labels; }
      else if (h.field == "kana") { kana_probability = std::move(probabilities); kana_labels = h.labels; }
      else if (h.field == "class") { class_probability.push_back(std::move(probabilities)); class_labels.push_back(h.labels); }
      else { number_probability.push_back(std::move(probabilities)); number_labels.push_back(h.labels); }
      offset += h.labels.size();
    }
    Choice base_region = single_choice(region_probability, region_labels, [&](const std::string& value) {
      return std::find(region_labels.begin() + std::min<size_t>(1, region_labels.size()), region_labels.end(), value) != region_labels.end();
    });
    Choice base_kana = single_choice(kana_probability, kana_labels, [](const std::string& value) {
      static const std::string kana = "あいうえかきくけこをさすせそたちつてとなにぬねのはひふほまみむめもやゆよらりるろわれ";
      return !value.empty() && kana.find(value) != std::string::npos;
    });
    Choice base_class = classification_choice(class_probability, class_labels);
    Choice base_serial = serial_choice(number_probability, number_labels);
    if (profile) {
      profile->primary_ocr += elapsed_ms(start);
      ++profile->ocr_reads;
    }
    const auto verified = [&](json reading) {
      if (!crop.empty()) {
        const auto begin = ProfileClock::now();
        const auto width = std::lround(b.w), height = std::lround(b.h);
        const bool narrow = width >= 40 && width < 55 && 10 * width >= 15 * height &&
                            10 * width <= 23 * height && b.score >= .45f;
        *evidence = numeric_verifier_->analyze(crop, number_probability, number_labels, reading, narrow,
                                               [&] { return expanded_digit_crop(f, W, H, b); }, profile);
        if (profile) profile->numeric_verification += elapsed_ms(begin);
      }
      return reading;
    };
    const auto auxiliary = [&](AuxReader* reader) {
      if (!reader) return Choice{};
      const auto start = ProfileClock::now();
      auto result = reader->read(f, W, H, b);
      if (profile) profile->auxiliary_ocr += elapsed_ms(start);
      return result;
    };
    if (text_ocr_ && refine) {
      const auto text_start = ProfileClock::now();
      const double auxiliary_before_refine = profile ? profile->auxiliary_ocr : 0;
      auto reading = text_ocr_->refine(f, W, H, b, base_class, base_kana, base_serial,
                                       [&] { return auxiliary(aux_kana_.get()); },
                                       [&] { return auxiliary(aux_class_.get()); }, profile, number_only);
      if (profile) profile->text += elapsed_ms(text_start) - (profile->auxiliary_ocr - auxiliary_before_refine);
      return verified(std::move(reading));
    }
    if (!refine) {
      auxiliary(aux_kana_.get());
      auxiliary(aux_class_.get());
    }
    return verified({{"region", base_region.confidence >= cfg_.field_threshold ? base_region.text : ""},
            {"class", base_class.confidence >= cfg_.field_threshold ? base_class.text : ""},
            {"kana", base_kana.confidence >= cfg_.field_threshold ? base_kana.text : ""},
            {"number", base_serial.text}, {"confidence", base_serial.confidence}});
  }

  Config cfg_;
  int threads_;
  TFLiteModel det_, ocr_;
  struct ShrinkingCrop {
    Box last;
    std::string serial;
    uint64_t sequence;
    int remaining;
  };
  std::optional<ShrinkingCrop> shrinking_crop_;
  struct RecentCrop {
    Box box;
    int consecutive, deferred;
  };
  std::vector<RecentCrop> recent_ocr_crops_;
  std::optional<Box> previous_warm_candidate_;
  std::unique_ptr<NumericVerifier> numeric_verifier_;
  std::unique_ptr<AuxReader> aux_kana_, aux_class_;
  std::unique_ptr<TextOCR> text_ocr_;
  int dw_ = 0, dh_ = 0, ow_ = 0, oh_ = 0, oc_ = 0;
};

Config load_config(const std::string& path) {
  std::ifstream in(path);
  if (!in) throw std::runtime_error("cannot open config");
  json j = json::parse(in);
  if (j.value("version", 0) != 1) throw std::runtime_error("unsupported config version");
  Config c;
  c.model_dir = std::filesystem::path(path).parent_path().string();
  if (j.contains("detector")) {
    auto& d = j["detector"];
    c.score = d.value("scoreThreshold", c.score);
    c.iou = d.value("iouThreshold", c.iou);
    c.max_plates = d.value("maxPlates", c.max_plates);
    c.min_width = d.value("minWidth", c.min_width);
    if (d.contains("roi")) c.roi = d.at("roi").get<std::array<int, 4>>();
    if (d.contains("sideRois")) {
      c.side_rois = d.at("sideRois").get<std::vector<std::array<int, 4>>>();
      if (c.side_rois.size() > 3) throw std::runtime_error("at most three rotating detection regions are supported");
    }
    if (d.contains("fallbackRoi")) {
      const auto roi = d.at("fallbackRoi").get<std::array<int, 4>>();
      if (roi[0] < 0 || roi[1] < 0 || roi[2] < 2 || roi[3] < 2) {
        throw std::runtime_error("fallbackRoi requires a nonnegative origin and dimensions of at least two pixels");
      }
      c.fallback_roi = roi;
    }
    c.ignore_recorder_overlay = d.value("ignoreRecorderOverlay", false);
  }
  auto& o = j.at("ocr");
  c.margin = o.value("margin", c.margin);
  c.field_threshold = o.value("fieldThreshold", c.field_threshold);
  c.full_ocr_every = o.value("fullOcrEvery", c.full_ocr_every);
  c.reread_shrinking_digits = o.value("rereadShrinkingDigits", c.reread_shrinking_digits);
  c.require_digit_detail = o.value("requireDigitDetail", c.require_digit_detail);
  c.reread_narrow_cool_crops = o.value("rereadNarrowCoolCrops", c.reread_narrow_cool_crops);
  c.reread_crowded_warm_crops = o.value("rereadCrowdedWarmCrops", c.reread_crowded_warm_crops);
  if (c.full_ocr_every < 1) throw std::runtime_error("fullOcrEvery must be positive");
  c.full_ocr_min_width = o.value("fullOcrMinWidth", c.full_ocr_min_width);
  if (c.full_ocr_min_width < 0 || c.full_ocr_min_width > 4096)
    throw std::runtime_error("fullOcrMinWidth must be between 0 and 4096");
  c.crowded_ocr_interval = o.value("crowdedOcrInterval", c.crowded_ocr_interval);
  if (c.crowded_ocr_interval < 1 || c.crowded_ocr_interval > 10)
    throw std::runtime_error("crowdedOcrInterval must be between 1 and 10");
  c.ocr_min_crop_width = o.value("minCropWidth", c.ocr_min_crop_width);
  if (c.ocr_min_crop_width < 0 || c.ocr_min_crop_width > 4096)
    throw std::runtime_error("minCropWidth must be between 0 and 4096");
  c.ocr_min_detection_score = o.value("minDetectionScore", c.ocr_min_detection_score);
  if (!(c.ocr_min_detection_score >= 0.f && c.ocr_min_detection_score <= 1.f))
    throw std::runtime_error("minDetectionScore must be between 0 and 1");
  c.field_readers = o.value("fieldReaders", json::object());
  c.numeric_verifier = o.value("numericVerifier", json::object());
  if (o.contains("textOcr")) {
    c.text_ocr = o.at("textOcr");
    c.region_threshold = c.text_ocr.value("regionThreshold", c.region_threshold);
    c.kana_threshold = c.text_ocr.value("kanaThreshold", c.kana_threshold);
  }
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

constexpr int shared_width = 1920, shared_height = 1080;
constexpr size_t shared_bytes = shared_width * shared_height * 3 / 2;

const uint8_t* map_frame_buffer() {
  const char* offered = std::getenv("NCC_PLATE_FRAME_FD");
  if (!offered || std::strcmp(offered, "3") != 0) return nullptr;
  constexpr int fd = 3;
  struct stat info{};
  void* mapping = MAP_FAILED;
  if (fstat(fd, &info) == 0 && S_ISREG(info.st_mode) && info.st_size == static_cast<off_t>(shared_bytes))
    mapping = mmap(nullptr, shared_bytes, PROT_READ, MAP_SHARED, fd, 0);
  close(fd);
  return mapping == MAP_FAILED ? nullptr : static_cast<const uint8_t*>(mapping);
}

bool read_exact(void* p, size_t n) { return std::fread(p, 1, n, stdin) == n; }

void emit(const json& j) {
  std::string s = j.dump(-1, ' ', false, json::error_handler_t::replace);
  s.push_back('\n');
  std::fwrite(s.data(), 1, s.size(), protocol_output);
  std::fflush(protocol_output);
}

}  // namespace

int main(int argc, char** argv) {
  const auto* shared_frame = map_frame_buffer();
  // Some vendor delegate libraries write their own diagnostics to stdout.
  // Keep those messages out of the JSON protocol read by the Go supervisor.
  const int protocol_fd = dup(STDOUT_FILENO);
  if (protocol_fd < 0) return 1;
  protocol_output = fdopen(protocol_fd, "w");
  if (!protocol_output || dup2(STDERR_FILENO, STDOUT_FILENO) < 0) return 1;

  std::string det, ocr, conf, gpu_cache;
  int threads = 2, bench = 0;
  float min_confidence = 0.3f;
  bool use_gpu = access("/dev/kgsl-3d0", R_OK | W_OK) == 0;
  GPU detector_gpu = GPU::cached_partition;
  bool prepare_gpu_cache = false;
  bool profile_frames = false;
  const auto initializing = std::chrono::steady_clock::now();
  for (int i = 1; i + 1 < argc; i += 2) {
    std::string k = argv[i], v = argv[i + 1];
    if (k == "--detector") det = v;
    else if (k == "--ocr") ocr = v;
    else if (k == "--config") conf = v;
    else if (k == "--threads") threads = std::clamp(std::atoi(v.c_str()), 1, 4);
    else if (k == "--min-confidence") min_confidence = std::clamp(std::strtof(v.c_str(), nullptr), 0.0f, 0.99f);
    else if (k == "--bench") bench = std::clamp(std::atoi(v.c_str()), 1, 1000);
    else if (k == "--profile") profile_frames = v == "1";
    else if (k == "--backend" && v == "cpu") use_gpu = false;
    else if (k == "--detector-backend") detector_gpu = v == "gpu" ? GPU::one_partition : GPU::off;
    else if (k == "--gpu-cache") gpu_cache = v;
    else if (k == "--prepare-gpu-cache") {
      gpu_cache = v;
      prepare_gpu_cache = true;
      detector_gpu = GPU::one_partition;
    }
  }
  const char* backend = std::getenv("NCC_PLATE_BACKEND");
  if (backend && std::strcmp(backend, "cpu") == 0) use_gpu = false;
  std::unique_ptr<Engine> engine;
  try {
    if (det.empty() || ocr.empty() || conf.empty()) throw std::runtime_error("usage: --detector D --ocr O --config C [--threads N]");
    if (gpu_cache.empty() && detector_gpu == GPU::cached_partition)
      gpu_cache = (std::filesystem::path(det).parent_path().parent_path() / "opencl-cache").string();
    engine = std::make_unique<Engine>(det, ocr, load_config(conf), threads,
                                     use_gpu ? detector_gpu : GPU::off, use_gpu, gpu_cache, prepare_gpu_cache,
                                     min_confidence);
    if (prepare_gpu_cache) engine->prepare_gpu_cache();
  } catch (const std::exception& e) {
    emit({{"ready", false}, {"error", e.what()}});
    return 1;
  }
  emit({{"ready", true}, {"engine", "tflite " TFLITE_VERSION_LABEL " + onnxruntime"},
        {"backend", engine->backend() + (engine->gpu_active() ? " + opencl gpu" : "")},
        {"initMs", std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - initializing).count()},
        {"sharedFrames", shared_frame != nullptr},
        {"latestModels", engine->latest_models()}});
  if (prepare_gpu_cache) return 0;
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
    const bool mapped = std::memcmp(header, "NPM1", 4) == 0;
    if ((!mapped && std::memcmp(header, "NPF1", 4) != 0) || fmt != 1 || w < 16 || h < 16 ||
        w > 4096 || h > 4096 || (w | h) & 1 || (mapped && (!shared_frame || w != shared_width || h != shared_height))) {
      std::fprintf(stderr, "invalid frame header\n");
      return 2;  // stream position is unknown; let the supervisor restart us
    }
    const uint8_t* pixels = shared_frame;
    if (!mapped) {
      frame.resize(static_cast<size_t>(w) * h * 3 / 2);
      if (!read_exact(frame.data(), frame.size())) return 0;
      pixels = frame.data();
    }
    auto t0 = std::chrono::steady_clock::now();
    json reply = {{"seq", seq}};
    try {
      FrameProfile profile;
      reply["plates"] = engine->process(pixels, static_cast<int>(w), static_cast<int>(h),
                                         seq, profile_frames ? &profile : nullptr);
      if (profile_frames) reply["profile"] = profile.to_json();
    } catch (const std::exception& e) {
      reply["plates"] = json::array();
      reply["error"] = e.what();
    }
    reply["ms"] = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
    reply["backend"] = engine->backend();
    emit(reply);
  }
  return 0;
}
