// Uses the engine's tensor helpers and serial decoder in plate_engine.cc.
// The aligner estimates four separate crops so the classifier can inspect each digit.
class LocalizedDigits {
 public:
  LocalizedDigits(const std::string& alignment, const std::string& reader, int threads)
      : align_(alignment, threads, GPU::off, {}, false), reader_(reader, threads, GPU::off, {}, false) {
    check_shape(align_, {1, 96, 192, 1}, {1, 20});
    check_shape(reader_, {4, 32, 24, 1}, {4, 11});
  }

  Choice detect(const std::vector<uint8_t>& image, const std::vector<std::vector<std::string>>& labels) {
    align_.invoke([&](auto* interpreter) {
      InputWriter input{interpreter->tensor(interpreter->inputs()[0])};
      for (size_t i = 0; i < image.size(); ++i) input.set(i, image[i] / 255.f);
    });
    const auto* out = align_->tensor(align_->outputs()[0]);
    std::array<float, 20> transform{};
    for (size_t i = 0; i < transform.size(); ++i) transform[i] = std::tanh(read_out(out, i));
    reader_.invoke([&](auto* interpreter) {
      InputWriter input{interpreter->tensor(interpreter->inputs()[0])};
      constexpr std::array<float, 4> centers{-.368f, -.054f, .318f, .642f};
      for (int digit = 0; digit < 4; ++digit) {
        const auto* p = transform.data() + 5 * digit;
        const float cx = centers[digit] + .137f * p[0], cy = .26f + .23f * p[1];
        const float sx = .155f * (1.f + .38f * p[2]), sy = .46f * (1.f + .28f * p[3]);
        const float tilt = .075f * p[4];
        for (int y = 0; y < 32; ++y) {
          const float v = 2.f * y / 31.f - 1.f;
          for (int x = 0; x < 24; ++x) {
            const float u = 2.f * x / 23.f - 1.f;
            const float fx = (1.f + cx + sx * u + tilt * v) * 95.5f;
            const float fy = (1.f + cy + sy * v + tilt * u) * 47.5f;
            input.set((digit * 32 + y) * 24 + x, sample(image, fx, fy));
          }
        }
      }
    });
    const auto* predicted = reader_->tensor(reader_->outputs()[0]);
    std::vector<std::vector<float>> slots;
    for (int i = 0; i < 4; ++i) slots.push_back(softmax(predicted, 11 * i, 11));
    return serial_choice(slots, labels);
  }

 private:
  static void check_shape(TFLiteModel& model, std::initializer_list<int> input, std::initializer_list<int> output) {
    const auto matches = [](const TfLiteTensor* tensor, std::initializer_list<int> shape) {
      return tensor->dims->size == static_cast<int>(shape.size()) &&
             std::equal(shape.begin(), shape.end(), tensor->dims->data);
    };
    if (!matches(model->tensor(model->inputs()[0]), input) || !matches(model->tensor(model->outputs()[0]), output))
      throw std::runtime_error("invalid localized digit model shape");
  }

  static float sample(const std::vector<uint8_t>& image, float fx, float fy) {
    fx = std::clamp(fx, 0.f, 191.f);
    fy = std::clamp(fy, 0.f, 95.f);
    const int x = static_cast<int>(fx), y = static_cast<int>(fy);
    const int x1 = std::min(x + 1, 191), y1 = std::min(y + 1, 95);
    const float dx = fx - x, dy = fy - y;
    const float top = (1.f - dx) * image[y * 192 + x] + dx * image[y * 192 + x1];
    const float bottom = (1.f - dx) * image[y1 * 192 + x] + dx * image[y1 * 192 + x1];
    return ((1.f - dy) * top + dy * bottom) / 255.f;
  }

  TFLiteModel align_, reader_;
};

class NumericVerifier {
 public:
  NumericVerifier(const std::string& path, const json& config, int threads)
      : model_(path, threads, GPU::off, {}, false),
        weights_(config.at("weights").get<std::array<float, 23>>()),
        intercept_(config.at("intercept").get<float>()) {
    if (!std::isfinite(intercept_) || std::any_of(weights_.begin(), weights_.end(),
                                                [](float value) { return !std::isfinite(value); })) {
      throw std::runtime_error("invalid numeric verifier calibration");
    }
    const auto* in = model_->tensor(model_->inputs()[0]);
    const auto* out = model_->tensor(model_->outputs()[0]);
    if (in->dims->size != 4 || in->dims->data[0] != 1 || in->dims->data[1] != 96 ||
        in->dims->data[2] != 192 || in->dims->data[3] != 1 || out->dims->size != 2 ||
        out->dims->data[0] != 1 || out->dims->data[1] != 308) {
      throw std::runtime_error("invalid numeric verifier tensor dimensions");
    }
    const auto directory = std::filesystem::path(path).parent_path();
    if (config.contains("localizedAlign") != config.contains("localizedRead"))
      throw std::runtime_error("localized digit verifier requires both models");
    if (config.contains("localizedAlign")) {
      const auto align = directory / config.at("localizedAlign").get<std::string>();
      const auto reader = directory / config.at("localizedRead").get<std::string>();
      localized_ = std::make_unique<LocalizedDigits>(align.string(), reader.string(), threads);
    }
    if (config.contains("referenceLayout")) {
      const auto file = directory / config.at("referenceLayout").get<std::string>();
      layout_ = std::make_unique<TFLiteModel>(file.string(), threads, GPU::off, std::string{}, false);
      const auto* src = (*layout_)->tensor((*layout_)->inputs()[0]);
      const auto* dst = (*layout_)->tensor((*layout_)->outputs()[0]);
      if (src->dims->size != 4 || src->dims->data[0] != 1 || src->dims->data[1] != 96 || src->dims->data[2] != 192 ||
          src->dims->data[3] != 1 || dst->dims->size != 2 || dst->dims->data[0] != 1 || dst->dims->data[1] < 44)
        throw std::runtime_error("invalid layout verifier tensor dimensions");
    }
  }

  json analyze(const std::vector<uint8_t>& image, const std::vector<std::vector<float>>& original,
               const std::vector<std::vector<std::string>>& labels, const json& reading) {
    model_.invoke([&](auto* interpreter) {
      InputWriter in{interpreter->tensor(interpreter->inputs()[0])};
      for (size_t i = 0; i < image.size(); ++i) in.set(i, image[i] / 255.f);
    });
    const auto* out = model_->tensor(model_->outputs()[0]);
    std::vector<std::vector<float>> physical;
    for (size_t i = 0; i < 4; ++i) physical.push_back(softmax(out, 11 * i, 11));
    const auto alternate = serial_choice(physical, labels);
    auto base = original;
    if (layout_) {
      layout_->invoke([&](auto* interpreter) {
        InputWriter in{interpreter->tensor(interpreter->inputs()[0])};
        for (size_t i = 0; i < image.size(); ++i) in.set(i, image[i] / 255.f);
      });
      const auto* result = (*layout_)->tensor((*layout_)->outputs()[0]);
      const size_t first = static_cast<size_t>(result->dims->data[1] - 44);
      for (size_t digit = 0; digit < 4; ++digit) base[digit] = softmax(result, first + 11 * digit, 11);
    }

    std::string sequence;
    std::vector<float> strength;
    size_t last = 0;
    for (size_t i = 0; i < 24; ++i) {
      const auto tokens = softmax(out, 44 + 11 * i, 11);
      const auto token = static_cast<size_t>(std::max_element(tokens.begin(), tokens.end()) - tokens.begin());
      if (token != 0 && token != last) {
        sequence += static_cast<char>('0' + token - 1);
        strength.push_back(tokens[token]);
      } else if (token != 0 && !strength.empty()) {
        strength.back() = std::max(strength.back(), tokens[token]);
      }
      last = token;
    }
    const float sequence_strength = strength.empty() ? 0.f : *std::min_element(strength.begin(), strength.end());
    json scores = json::object();
    const auto image_metrics = crop_metrics(image);
    for (const auto& n : {reading.value("number", std::string{}), alternate.text, sequence}) {
      if (n.empty() || n.size() > 4 || n.front() == '0' || n.find_first_not_of("0123456789") != std::string::npos) continue;
      const auto a = number_metrics(n, base);
      const auto b = number_metrics(n, physical);
      const auto mismatch = std::inner_product(n.begin(), n.begin() + std::min(n.size(), sequence.size()),
                                               sequence.begin(), 0, std::plus<int>(), std::not_equal_to<char>());
      std::array<float, 23> features{};
      std::copy(a.begin(), a.end(), features.begin());
      std::copy(b.begin(), b.end(), features.begin() + 6);
      features[12] = n == sequence ? 1.f : 0.f;
      features[13] = static_cast<float>(std::max(n.size(), sequence.size()) - std::min(n.size(), sequence.size()));
      features[14] = static_cast<float>(mismatch);
      features[15] = sequence_strength;
      std::copy(image_metrics.begin(), image_metrics.end(), features.begin() + 16);
      features[22] = static_cast<float>(n.size());
      const float logit = std::inner_product(weights_.begin(), weights_.end(), features.begin(), intercept_);
      scores[n] = 1.f / (1.f + std::exp(-std::clamp(logit, -60.f, 60.f)));
    }
    auto verifier = reading;
    verifier["number"] = alternate.text;
    verifier["confidence"] = alternate.confidence;
    json result = {{"numberScores", std::move(scores)}, {"verifier", std::move(verifier)}};
    if (result["numberScores"].contains(sequence) && result["numberScores"][sequence].get<float>() > .60f)
      result["ctc"] = sequence;
    if (localized_) {
      const auto check = localized_->detect(image, labels);
      if (check.confidence >= .72f)
        result["localized"] = {{"number", check.text}, {"confidence", check.confidence}};
    }
    return result;
  }

 private:
  static std::array<float, 6> number_metrics(const std::string& number,
                                            const std::vector<std::vector<float>>& values) {
    const auto digits = std::string(4 - number.size(), ' ') + number;
    std::array<float, 4> score{}, margins{};
    size_t below = 0;
    for (size_t i = 0; i < 4; ++i) {
      const size_t wanted = digits[i] == ' ' ? 0 : static_cast<size_t>(digits[i] - '0') + 1;
      const auto& slot = values[i];
      score[i] = slot[wanted];
      float other = 0.f;
      for (size_t j = 0; j < slot.size(); ++j)
        if (j != wanted) other = std::max(other, slot[j]);
      margins[i] = score[i] - other;
      below += margins[i] < 0;
    }
    std::sort(score.begin(), score.end());
    return {*std::min_element(margins.begin(), margins.end()), std::accumulate(margins.begin(), margins.end(), 0.f) / 4,
            score[0], score[1], std::accumulate(score.begin(), score.end(), 0.f) / 4, static_cast<float>(below)};
  }

  static std::array<float, 6> crop_metrics(const std::vector<uint8_t>& image) {
    constexpr int width = 145, height = 51;
    std::array<int, 256> counts{};
    int gradient_x = 0, gradient_y = 0, saturated = 0;
    for (int y = 0; y < height; ++y) {
      for (int x = 0; x < width; ++x) {
        const size_t i = (y + 36) * 192 + x + 36;
        const int level = image[i];
        ++counts[level];
        saturated += level <= 6 || level >= 248;
        if (x > 0) {
          const int d = level - image[i - 1];
          gradient_x += d * d;
        }
        if (y > 0) {
          const int d = level - image[i - 192];
          gradient_y += d * d;
        }
      }
    }
    const auto percentile = [&](float p) {
      const float pos = p * (width * height - 1);
      const int lo = static_cast<int>(pos), hi = static_cast<int>(std::ceil(pos));
      const auto at = [&](int rank) {
        int below = 0;
        for (int value = 0; value < 256; ++value) {
          below += counts[value];
          if (below > rank) return value;
        }
        return 255;
      };
      return (at(lo) + (pos - lo) * (at(hi) - at(lo))) / 255.f;
    };
    const float gx = gradient_x / (255.f * 255.f * (width - 1) * height);
    const float gy = gradient_y / (255.f * 255.f * width * (height - 1));
    return {gx, gy, saturated / static_cast<float>(width * height), percentile(.92f) - percentile(.08f),
            percentile(.50f), gx / (.0002f + gy)};
  }

  TFLiteModel model_;
  const std::array<float, 23> weights_;
  const float intercept_;
  std::unique_ptr<LocalizedDigits> localized_;
  std::unique_ptr<TFLiteModel> layout_;
};
