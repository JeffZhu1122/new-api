export const meta = {
  apiVersion: 1,
  key: "kling",
  name: "Kling",
  icon: "Kling.Color",
  description: {
    en: "Kuaishou Kling video generation (text-to-video, image-to-video and omni reference)",
    zh: "快手可灵视频生成（文生视频、图生视频、全能参考生视频）",
  },
  version: "1.3.1",
  author: { name: "QuantumNous" },
  channelTypes: [50],
  models: [
    "kling-v2-5-turbo",
    "kling-v2-6",
    "kling-v3",
    "kling-2.5-turbo",
    "kling-2.6",
    "kling-3.0",
    "kling-3.0-turbo",
    "kling-3.0-omni",
    "kling-4.0",
    "kling-4.0-flash",
  ],
  fetchMode: "per_task",
  upstreams: ["vendor", "new_api"],
  usageSchema: {
    // Kling final unit deduction (estimated at submit, actual on completion).
    units: {
      type: "number",
      unit: "credit",
      description: { en: "Kling credit unit price", zh: "可灵资源包单位单价" },
    },
  },
  usageExamples: [
    { label: "v2.5 turbo std 5s", facts: { units: 1.5 } },
    { label: "v2.6 pro 10s · audio", facts: { units: 10 } },
    { label: "v3 pro 5s", facts: { units: 4 } },
    { label: "v3 4k 5s", facts: { units: 15 } },
    { label: "4.0 1080p 10s", facts: { units: 15 } },
    { label: "4.0 1080p 10s · video input", facts: { units: 18 } },
    { label: "4.0 4K 5s · HDR", facts: { units: 20 } },
    { label: "4.0 Flash 720p 10s", facts: { units: 6 } },
    { label: "3.0 1080p 5s · audio", facts: { units: 6 } },
    { label: "3.0 Omni 1080p 5s · reference video", facts: { units: 6 } },
  ],
  protocols: [{ name: "openai_responses", supports: ["stream", "sync", "background"] }, "openai_video"],
  // Kling's own paths; the gateway also serves each under /kling.
  routes: [
    { method: "POST", path: "/v1/videos/text2video", type: "submit", action: "text_to_video", decode: "decodeSubmit", render: "taskCreated" },
    { method: "POST", path: "/v1/videos/image2video", type: "submit", action: "image_to_video", decode: "decodeSubmit", render: "taskCreated" },
    { method: "GET", path: "/v1/videos/text2video/:task_id", type: "query", render: "taskStatus" },
    { method: "GET", path: "/v1/videos/image2video/:task_id", type: "query", render: "taskStatus" },
    // Kling's model-in-path API names the model in the last segment and
    // answers every task at GET /tasks?task_ids=.
    { method: "POST", path: "/text-to-video/:model", type: "submit", action: "text_to_video", decode: "pathTextToVideo", render: "pathTaskCreated" },
    { method: "POST", path: "/image-to-video/:model", type: "submit", action: "image_to_video", decode: "pathImageToVideo", render: "pathTaskCreated" },
    { method: "POST", path: "/omni-video/:model", type: "submit", action: "reference_to_video", decode: "pathOmniVideo", render: "pathTaskCreated" },
    { method: "GET", path: "/tasks", type: "query", taskIdParam: "task_ids", render: "pathTasks" },
  ],
};

// The official resource-package consumption of Kling's models in units per
// output second of each mode, not a currency price
// (kling.ai/document-api/pricing/base/video): silent, with native audio (sound
// "on"), and with a reference voice (voice_list). Kling checks modes and
// durations itself and reports the units it deducted as final_unit_deduction,
// which settles the task.
const KLING_MODELS = {
  "kling-v2-5-turbo": { units: { std: 0.3, pro: 0.5 } },
  "kling-v2-6": { units: { std: 0.3, pro: 0.5 }, soundUnits: { pro: 1.0 }, voiceUnits: { pro: 1.2 } },
  "kling-v3": { units: { std: 0.6, pro: 0.8, "4k": 3.0 }, soundUnits: { std: 0.9, pro: 1.2, "4k": 3.0 } },
};

// A channel may map a declared model to a name outside the table; it reserves
// the highest consumption of each mode and settles on the units Kling reports.
const DEFAULT_KLING_MODEL = {
  units: { std: 0.9, pro: 1.2, "4k": 3.0 },
  soundUnits: { std: 0.9, pro: 1.2, "4k": 3.0 },
  voiceUnits: { std: 1.2, pro: 1.2, "4k": 3.0 },
};

// The text-to-video aspect ratios of kling-v2-5-turbo, kling-v2-6, kling-v3
// and the 2.x and 3.x models of the model-in-path API, then of Kling 4.0 and
// 4.0 Flash. The host protocols send the one nearest the request's size;
// without a size Kling uses its default, 16:9.
const KLING_ASPECT_RATIOS = ["16:9", "9:16", "1:1"];
const KLING4_ASPECT_RATIOS = ["16:9", "9:16", "1:1", "21:9", "4:3", "3:4"];

const TEXT_TO_VIDEO = "text-to-video";
const IMAGE_TO_VIDEO = "image-to-video";
const OMNI_VIDEO = "omni-video";
const PATH_ACTIONS = { "text-to-video": "text_to_video", "image-to-video": "image_to_video", "omni-video": "reference_to_video" };

// Kling's model-in-path API (kling.ai/document-api/api/video/...): the model
// is the last path segment, it takes only an API key, and its units per
// second (kling.ai/document-api/pricing/base/video) depend on the resolution.
// Each model lists the endpoints it serves, its resolutions with Kling's
// default first, and its durations: a range, or the values it accepts.
//
// Kling 4.0 and 4.0 Flash bill a total duration (`total`): the output alone,
// or, with an input video, twice the output, or input plus output when the
// input is longer. Their input videos are at most as long as their longest
// output, and without a duration Kling follows the prompt or the base_video.
// Kling 4.0 also makes HDR at 1080p and 4K (settings.dynamic_range "hdr"),
// which adds `hdr` units per second of the output alone, on top of the total.
//
// The other models bill the output at the rate of what they make (`units`):
// `base`, with native audio (`audio`), with a reference voice (`voice`), or,
// on 3.0 Omni, with a reference video (`videoInput`), whatever the audio.
// Kling 3.0 Turbo always makes audio and has one rate. Without a duration
// Kling makes `seconds.default`.
const PATH_MODELS = {
  "kling-4.0": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO, OMNI_VIDEO],
    resolutions: ["1080p", "720p", "4k"],
    seconds: { min: 3, max: 30 },
    aspectRatios: KLING4_ASPECT_RATIOS,
    total: { units: { "720p": 1.0, "1080p": 1.5, "4k": 3.5 }, videoInput: { "720p": 0.6, "1080p": 0.9, "4k": 2.1 } },
    hdr: { "1080p": 0.3, "4k": 0.5 },
  },
  "kling-4.0-flash": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO, OMNI_VIDEO],
    resolutions: ["720p"],
    seconds: { min: 3, max: 20 },
    aspectRatios: KLING4_ASPECT_RATIOS,
    total: { units: { "720p": 0.6 }, videoInput: { "720p": 0.4 } },
  },
  "kling-3.0": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO],
    resolutions: ["720p", "1080p", "4k"],
    seconds: { min: 3, max: 15, default: 5 },
    aspectRatios: KLING_ASPECT_RATIOS,
    units: { base: { "720p": 0.6, "1080p": 0.8, "4k": 3.0 }, audio: { "720p": 0.9, "1080p": 1.2, "4k": 3.0 } },
  },
  "kling-3.0-omni": {
    endpoints: [OMNI_VIDEO],
    resolutions: ["720p", "1080p", "4k"],
    seconds: { min: 3, max: 15, default: 5 },
    aspectRatios: KLING_ASPECT_RATIOS,
    units: {
      base: { "720p": 0.6, "1080p": 0.8, "4k": 3.0 },
      audio: { "720p": 0.8, "1080p": 1.0, "4k": 3.0 },
      videoInput: { "720p": 0.9, "1080p": 1.2, "4k": 3.0 },
    },
  },
  "kling-3.0-turbo": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO],
    resolutions: ["720p", "1080p"],
    seconds: { min: 3, max: 15, default: 5 },
    aspectRatios: KLING_ASPECT_RATIOS,
    units: { base: { "720p": 0.8, "1080p": 1.0 } },
  },
  "kling-2.6": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO],
    resolutions: ["720p", "1080p"],
    seconds: { values: [5, 10], default: 5 },
    aspectRatios: KLING_ASPECT_RATIOS,
    units: { base: { "720p": 0.3, "1080p": 0.5 }, audio: { "1080p": 1.0 }, voice: { "1080p": 1.2 } },
  },
  "kling-2.5-turbo": {
    endpoints: [TEXT_TO_VIDEO, IMAGE_TO_VIDEO],
    resolutions: ["720p", "1080p"],
    seconds: { values: [5, 10], default: 5 },
    aspectRatios: KLING_ASPECT_RATIOS,
    units: { base: { "720p": 0.3, "1080p": 0.5 } },
  },
};
// The shorter side of each resolution, for the one nearest a size.
const RESOLUTION_SIDES = { "720p": 720, "1080p": 1080, "4k": 2160 };
// The earlier API's modes that ask for more than Kling's default resolution.
const MODE_RESOLUTIONS = { pro: "1080p", "4k": "4k" };
// Whether an element is a video character is not in the request, so an
// element counts as an input video.
const PATH_VIDEO_INPUTS = ["feature_video", "base_video", "element"];

// Kling's numeric error codes for the statuses the gateway answers with
// (kling.ai/document-api/api/get-started/error-codes).
const ERROR_CODES = { 400: 1201, 401: 1000, 403: 1103, 404: 1203, 429: 1302, 500: 5000, 503: 5001, 504: 5002 };

// Kling would call these back with its own task ids, which the gateway does
// not hand out, so a native request carrying them is refused.
const UNSUPPORTED_FIELDS = ["callback_url", "external_task_id"];

function trimmed(value) {
  return String(value || "").trim();
}

function responsesInput(req) {
  const texts = [],
    images = [];
  const input = req.input;
  if (typeof input === "string") texts.push(input);
  else if (Array.isArray(input)) {
    for (const item of input) {
      if (typeof item === "string") {
        texts.push(item);
        continue;
      }
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
      for (const part of content) {
        if (typeof part === "string") {
          texts.push(part);
          continue;
        }
        if (!part || typeof part !== "object" || Array.isArray(part)) continue;
        if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
        if (["input_image", "image_url"].includes(part.type)) {
          let image = part.image_url;
          if (image && typeof image === "object") image = image.url;
          if (trimmed(image)) images.push(trimmed(image));
        }
      }
    }
  }
  return {
    prompt: texts
      .filter(function (text) {
        return trimmed(text);
      })
      .join("\n"),
    images: images,
  };
}

function responsesVideoText(ctx) {
  const artifact = ctx && ctx.artifacts && ctx.artifacts.video;
  const url = trimmed(artifact && artifact.url);
  if (!url) throw new Error("video artifact is unavailable");
  const escaped = url.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return '<video controls src="' + escaped + '"></video>';
}

function isRelay(apiKey) {
  return apiKey.startsWith("sk-");
}

// The host signal is authoritative on New API channels; the sk- key prefix
// stays as the heuristic for legacy type-50 channels pointed at a gateway.
function viaGateway(ctx) {
  return !!(ctx.upstream && ctx.upstream.kind === "new_api") || isRelay(ctx.apiKey);
}

// The earlier API takes an API key or a JWT signed with accessKey|secretKey;
// the model-in-path API refuses the JWT.
function tokenFor(ctx, modelPath) {
  const apiKey = trimmed(ctx.apiKey);
  if (!apiKey) throw new Error("api_key is empty");
  if (viaGateway(ctx)) return apiKey;
  const parts = apiKey.split("|");
  if (parts.length === 1) return apiKey;
  if (modelPath) throw new Error("this Kling model takes an API key, not accessKey|secretKey; create an API key in the Kling console");
  if (parts.length !== 2) throw new Error("invalid api_key, required format is accessKey|secretKey");
  const now = utils.unixNow();
  return utils.jwtSignHS256({ iss: parts[0].trim(), exp: now + 1800, nbf: now - 5 }, parts[1].trim());
}

function pathFor(action) {
  return action === "image_to_video" ? "/v1/videos/image2video" : "/v1/videos/text2video";
}

// Another gateway serves Kling under /kling. A Base URL that already ends with
// /kling is that address, so it is not added twice.
function apiRoot(ctx) {
  const base = trimmed(ctx.baseUrl).replace(/\/+$/, "");
  if (!viaGateway(ctx) || /\/kling$/i.test(base)) return base;
  return base + "/kling";
}

function urlFor(ctx, action) {
  return apiRoot(ctx) + pathFor(action);
}

function requestHeaders(ctx, modelPath) {
  return { Accept: "application/json", Authorization: "Bearer " + tokenFor(ctx, modelPath), "User-Agent": "kling-sdk/1.0" };
}

// A host-protocol size, WIDTHxHEIGHT.
function sizeDimensions(size) {
  const parts = trimmed(size).toLowerCase().split("x");
  const width = Number(parts[0]);
  const height = Number(parts[1]);
  if (parts.length !== 2 || !(width > 0) || !(height > 0)) throw new Error("size must be WIDTHxHEIGHT, such as 1280x720");
  return { width: width, height: height };
}

// The aspect ratio of the list nearest the size's.
function aspectRatio(dimensions, ratios) {
  let nearest = ratios[0];
  let distance = Infinity;
  for (const ratio of ratios) {
    const sides = ratio.split(":").map(Number);
    const ratioDistance = Math.abs(Math.log(dimensions.width / dimensions.height) - Math.log(sides[0] / sides[1]));
    if (ratioDistance < distance) {
      nearest = ratio;
      distance = ratioDistance;
    }
  }
  return nearest;
}

function submitModel(ctx, req) {
  return trimmed((ctx && ctx.upstreamModel) || (ctx && ctx.model) || (req && req.model));
}

function klingModel(model) {
  return Object.prototype.hasOwnProperty.call(KLING_MODELS, model) ? KLING_MODELS[model] : DEFAULT_KLING_MODEL;
}

// mode defaults to std; Kling checks it for the model.
function klingMode(mode) {
  return trimmed(mode).toLowerCase() || "std";
}

// Kling takes duration as a string and checks it for the model; a duration is
// billed, so the plugin bounds it first.
function validateKlingDuration(value) {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (!Number.isInteger(n) || n <= 0 || n > 3600) throw new Error("duration must be a positive integer of at most 3600");
  return n;
}

// A model-in-path model makes only its own durations, so another one is
// refused before it is reserved.
function pathDuration(spec, value) {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  const seconds = spec.seconds;
  if (seconds.values) {
    if (!seconds.values.includes(n)) throw new Error("duration must be one of " + seconds.values.join(", "));
    return n;
  }
  if (!Number.isInteger(n) || n < seconds.min || n > seconds.max) throw new Error("duration must be an integer from " + seconds.min + " to " + seconds.max);
  return n;
}

// The duration Kling receives and the one billed: metadata.duration (the whole
// body on native routes) overrides the top-level duration, and Kling's default
// is 5.
function outboundDuration(req) {
  const metadata = (req && req.metadata) || {};
  let value = metadata.duration;
  if (value === undefined || value === null) value = req && req.duration;
  if (value === undefined || value === null) return 5;
  return validateKlingDuration(value);
}

function outboundMode(req) {
  const metadata = (req && req.metadata) || {};
  return klingMode((req && req.mode) || metadata.mode);
}

// Units per output second: native audio (sound "on", default "off") and a
// reference voice cost more. A mode the model's table does not list reserves
// the table's dearest, and audio the model has no table for the dearest audio
// of any model; the units Kling reports settle the task.
function unitsPerSecond(model, mode, metadata) {
  const profile = klingModel(model);
  let table = profile.units;
  if (trimmed(metadata.sound).toLowerCase() === "on") {
    const voice = Array.isArray(metadata.voice_list) && metadata.voice_list.length > 0;
    table = (voice && profile.voiceUnits) || profile.soundUnits || (voice ? DEFAULT_KLING_MODEL.voiceUnits : DEFAULT_KLING_MODEL.soundUnits);
  }
  if (Object.prototype.hasOwnProperty.call(table, mode)) return table[mode];
  return Object.keys(table).reduce((dearest, key) => Math.max(dearest, table[key]), 0);
}

function hasKlingImage(req, hasInputReferenceFile) {
  if (hasInputReferenceFile) return true;
  const metadata = (req && req.metadata) || {};
  if (req && req.image && typeof req.image === "object" && !Array.isArray(req.image) && req.image.__fileRef) return true;
  return Boolean(trimmed(req && req.input_reference) || trimmed(req && req.image) || metadata.image || metadata.image_tail);
}

function filePlaceholder(image) {
  if (!image || typeof image !== "object" || Array.isArray(image) || !image.__fileRef) return image;
  const placeholder = { __fileRef: image.__fileRef, encoding: image.encoding };
  if (image.mimeType) placeholder.mimeType = image.mimeType;
  if (image.maxBytes !== undefined && image.maxBytes !== null) placeholder.maxBytes = image.maxBytes;
  return placeholder;
}

function plainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function pathSpec(model) {
  return Object.prototype.hasOwnProperty.call(PATH_MODELS, model) ? PATH_MODELS[model] : null;
}

function hasValue(value) {
  return value !== undefined && value !== null;
}

// A native body goes to the API of the route the client called; the host
// protocols pick it by model.
function usesModelPath(ctx, req) {
  return req.vendorBody ? Boolean(req.endpoint) : Boolean(pathSpec(submitModel(ctx, req)));
}

// The resolution billed: Kling's default without one, else the one sent. A
// resolution the model does not list is billed at its dearest rate.
function billedResolution(spec, settings) {
  return trimmed(settings.resolution).toLowerCase() || spec.resolutions[0];
}

// The dynamic range billed: Kling's default, SDR, without one.
function billedDynamicRange(settings) {
  return trimmed(settings.dynamic_range).toLowerCase() || "sdr";
}

function rateFor(table, resolution) {
  if (Object.prototype.hasOwnProperty.call(table, resolution)) return table[resolution];
  return Object.keys(table).reduce((dearest, key) => Math.max(dearest, table[key]), 0);
}

// The model's resolution whose shorter side is nearest the size's; a tie takes
// the larger.
function nearestResolution(dimensions, resolutions) {
  const side = Math.min(dimensions.width, dimensions.height);
  let nearest = resolutions[0];
  let distance = Infinity;
  for (const resolution of resolutions) {
    const resolutionDistance = Math.abs(side - RESOLUTION_SIDES[resolution]);
    if (resolutionDistance < distance || (resolutionDistance === distance && RESOLUTION_SIDES[resolution] > RESOLUTION_SIDES[nearest])) {
      nearest = resolution;
      distance = resolutionDistance;
    }
  }
  return nearest;
}

function hasVideoInput(contents) {
  return contents.some(function (item) {
    return item && PATH_VIDEO_INPUTS.includes(item.type);
  });
}

// Kling would call back, or look tasks up, by ids the gateway does not hand
// out. The duration, resolution and dynamic range are billed, so they are kept
// to what the model makes and has a price for.
function checkPathBody(spec, body) {
  const options = plainObject(body.options);
  for (const name of UNSUPPORTED_FIELDS) {
    const value = options[name];
    if (value !== undefined && value !== null && value !== "") throw new Error("options." + name + " is not supported; query the task instead");
  }
  const settings = plainObject(body.settings);
  if (hasValue(settings.duration)) pathDuration(spec, settings.duration);
  const resolution = billedResolution(spec, settings);
  if (!spec.resolutions.includes(resolution)) throw new Error("settings.resolution must be one of " + spec.resolutions.join(", "));
  const dynamicRange = billedDynamicRange(settings);
  if (dynamicRange !== "sdr" && dynamicRange !== "hdr") throw new Error("settings.dynamic_range must be sdr or hdr");
  if (dynamicRange === "hdr" && !(spec.hdr && spec.hdr[resolution])) {
    if (!spec.hdr) throw new Error("settings.dynamic_range hdr is not available for this model");
    throw new Error("settings.dynamic_range hdr needs settings.resolution " + Object.keys(spec.hdr).join(" or "));
  }
}

// The model-in-path request: a native body as the client wrote it, for the
// endpoint it called. The host protocols send text-to-video, or image-to-video
// with the first frame and an optional last frame, or omni-video with the
// same contents for a model that has only that endpoint; metadata adds Kling's
// own fields. The earlier API's pro and 4k modes ask for that resolution and
// sound "on" for native audio, when the model has them. Kling 4.0 and 4.0
// Flash pick the length when the request has none; the other models get their
// default duration.
function pathSubmission(ctx) {
  const req = ctx.requestBody || {};
  const spec = pathSpec(submitModel(ctx, req));
  if (!spec) throw new Error("model " + submitModel(ctx, req) + " is not a Kling model-in-path model");
  if (req.vendorBody) {
    checkPathBody(spec, req.vendorBody);
    return { endpoint: req.endpoint, body: req.vendorBody };
  }
  const metadata = Object.assign({}, plainObject(req.metadata));
  const frames = [
    ["first_frame", req.image || metadata.image],
    ["last_frame", metadata.image_tail],
  ].filter(function (frame) {
    return frame[1];
  });
  let duration = metadata.duration;
  if (!hasValue(duration)) duration = req.duration;
  const mode = klingMode(req.mode || metadata.mode);
  const sound = trimmed(metadata.sound).toLowerCase();
  for (const name of ["mode", "sound", "duration", "image", "image_tail"]) delete metadata[name];
  let endpoint = frames.length ? IMAGE_TO_VIDEO : TEXT_TO_VIDEO;
  if (!spec.endpoints.includes(endpoint)) endpoint = OMNI_VIDEO;
  const settings = {};
  const size = trimmed(req.size);
  if (size) {
    const dimensions = sizeDimensions(size);
    settings.resolution = nearestResolution(dimensions, spec.resolutions);
    if (!frames.length) settings.aspect_ratio = aspectRatio(dimensions, spec.aspectRatios);
  } else if (!spec.total && spec.resolutions.includes(MODE_RESOLUTIONS[mode])) settings.resolution = MODE_RESOLUTIONS[mode];
  if (sound === "on" && spec.units && spec.units.audio) settings.audio = "native";
  if (hasValue(duration)) settings.duration = pathDuration(spec, duration);
  else if (!spec.total) settings.duration = spec.seconds.default;
  let body;
  if (endpoint === TEXT_TO_VIDEO) body = req.prompt ? { prompt: req.prompt } : {};
  else {
    const contents = req.prompt ? [{ type: "prompt", text: req.prompt }] : [];
    for (const frame of frames) contents.push({ type: frame[0], url: filePlaceholder(frame[1]) });
    body = { contents: contents };
  }
  if (Object.keys(settings).length) body.settings = settings;
  Object.assign(body, metadata);
  checkPathBody(spec, body);
  return { endpoint: endpoint, body: body };
}

// Kling 4.0 and 4.0 Flash follow the prompt or the base_video without a
// duration, so their longest output is reserved, and only Kling knows the
// length of an input video, so their longest input is reserved. The other
// models make their default duration. The units Kling reports settle the
// task.
function pathReservedUnits(spec, body) {
  const settings = plainObject(body.settings);
  const resolution = billedResolution(spec, settings);
  const contents = Array.isArray(body.contents) ? body.contents : [];
  const videoInput = hasVideoInput(contents);
  if (spec.total) {
    const seconds = hasValue(settings.duration) ? pathDuration(spec, settings.duration) : spec.seconds.max;
    const hdr = billedDynamicRange(settings) === "hdr" ? spec.hdr[resolution] * seconds : 0;
    if (!videoInput) return rateFor(spec.total.units, resolution) * seconds + hdr;
    return rateFor(spec.total.videoInput, resolution) * Math.max(seconds * 2, spec.seconds.max + seconds) + hdr;
  }
  const seconds = hasValue(settings.duration) ? pathDuration(spec, settings.duration) : spec.seconds.default;
  const units = spec.units;
  let table = units.base;
  if (videoInput && units.videoInput) table = units.videoInput;
  else if (trimmed(settings.audio).toLowerCase() === "native") {
    const voice = contents.some(function (item) {
      return item && item.type === "voice";
    });
    table = (voice && units.voice) || units.audio || units.base;
  }
  return rateFor(table, resolution) * seconds;
}

// The task's entry in a model-in-path reply: the list GET /tasks answers, or
// the single task of the create reply.
function pathEntry(reply, taskId) {
  const data = plainObject(reply).data;
  if (!Array.isArray(data)) return plainObject(data);
  return (
    data.find(function (entry) {
      return entry && taskId && entry.id === taskId;
    }) || plainObject(data[0])
  );
}

// A model-in-path reply carries the task as data, or as the only entry of the
// list GET /tasks answers; the earlier API's reply has data.task_id.
function isPathReply(reply) {
  const data = plainObject(reply).data;
  return Array.isArray(data) || Boolean(data && typeof data === "object" && data.id && !data.task_id);
}

function pathVideos(entry) {
  return (Array.isArray(entry.outputs) ? entry.outputs : []).filter(function (output) {
    return output && output.type === "video";
  });
}

// Upstream durations are saturated before they become units.
function reportedSeconds(value) {
  const seconds = Number.parseFloat(value);
  return Number.isFinite(seconds) && seconds > 0 ? Math.min(seconds, 3600) : 0;
}

// The units Kling deducted: a resource package's amount, or a cash deduction's
// list_price, not its amount, which is the discounted price. A unit lists at
// one yuan, so only a deduction in CNY converts; one in another currency has
// no fixed rate to units. Without a deduction that converts, Kling 4.0 and 4.0
// Flash settle on the durations Kling bills (usage.output_duration; the
// video's own duration runs slightly longer) at the resolution and dynamic
// range saved at submission, which the reply does not report, and the other
// models keep their reservation, which their request fixes.
function pathCompletedUnits(ctx, body) {
  const entry = pathEntry(body, ctx && ctx.taskId);
  if (entry.status !== "succeeded") return null;
  const deductions = Array.isArray(entry.billing) ? entry.billing : [];
  const deducted = deductions.reduce(function (sum, item) {
    const type = item && item.charge_type;
    let units = Number.NaN;
    if (type === "unit") units = Number.parseFloat(item.amount);
    else if (type === "cash" && trimmed(item.currency).toUpperCase() === "CNY") units = Number.parseFloat(item.list_price);
    return units >= 0 ? sum + units : Number.NaN;
  }, 0);
  if (deductions.length && Number.isFinite(deducted)) return { units: deducted };
  const spec = pathSpec(submitModel(ctx, null));
  if (!spec || !spec.total) return null;
  const usage = plainObject(entry.usage);
  let output = reportedSeconds(usage.output_duration);
  if (!output) {
    const videos = pathVideos(entry);
    output = reportedSeconds(videos.length ? videos[0].duration : "");
  }
  if (!output) return null;
  const input = reportedSeconds(usage.input_duration);
  const state = plainObject(ctx && ctx.state);
  const resolution = billedResolution(spec, state);
  const hdr = state.dynamicRange === "hdr" ? ((spec.hdr && spec.hdr[resolution]) || 0) * output : 0;
  if (!input) return { units: rateFor(spec.total.units, resolution) * output + hdr };
  return { units: rateFor(spec.total.videoInput, resolution) * (input <= output ? output * 2 : input + output) + hdr };
}

// A model-in-path route names the model in its last segment.
function decodePathSubmit(endpoint) {
  return function (ctx) {
    if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
    const body = ctx.body.value;
    if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("request body must be an object");
    const model = trimmed((ctx.params || {}).model);
    const spec = pathSpec(model);
    if (!spec || !spec.endpoints.includes(endpoint)) {
      const served = Object.keys(PATH_MODELS).filter(function (name) {
        return PATH_MODELS[name].endpoints.includes(endpoint);
      });
      throw new Error("Kling does not serve " + (model || "an empty model") + " at /" + endpoint + "; use one of " + served.join(", "));
    }
    checkPathBody(spec, body);
    return { kind: "submit", model: model, requestBody: { model: model, vendorBody: body, endpoint: endpoint } };
  };
}

// Kling's own body, whose model_name is required, sent as is for Kling to
// check.
function decodeNativeSubmit(ctx) {
  if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
  const body = ctx.body.value;
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("request body must be an object");
  const model = typeof body.model_name === "string" ? body.model_name.trim() : "";
  if (!model) throw new Error("model_name is required");
  for (const name of UNSUPPORTED_FIELDS) {
    if (body[name] !== undefined && body[name] !== null) throw new Error(name + " is not supported; query the task instead");
  }
  if (body.duration !== undefined && body.duration !== null) validateKlingDuration(body.duration);
  return { kind: "submit", model: model, requestBody: { model: model, vendorBody: body } };
}

export const native = {
  decodeSubmit: decodeNativeSubmit,
  taskCreated: function (ctx, task) {
    const result = task.data || {},
      data = result.data || {};
    return Object.assign({}, result, { data: Object.assign({}, data, { task_id: task.task_id }) });
  },
  // Kling's last reply for the task. The gateway can fail a task Kling still
  // reports as running (poll failures or the task timeout); it then reads failed.
  taskStatus: function (ctx, task) {
    const statusMap = { NOT_START: "submitted", SUBMITTED: "submitted", QUEUED: "submitted", IN_PROGRESS: "processing", SUCCESS: "succeed", FAILURE: "failed" };
    const result = task.data && typeof task.data === "object" && !Array.isArray(task.data) && !isPathReply(task.data) ? task.data : null;
    if (result) {
      const data = Object.assign({}, result.data && typeof result.data === "object" ? result.data : {}, { task_id: task.task_id });
      if (task.status === "FAILURE" && data.task_status !== "failed") {
        data.task_status = "failed";
        data.task_status_msg = task.fail_reason || "";
      }
      return Object.assign({}, result, { data: data });
    }
    return {
      code: 0,
      message: "",
      request_id: "",
      data: {
        task_id: task.task_id,
        task_status: statusMap[task.status] || "submitted",
        task_status_msg: task.fail_reason || "",
        created_at: task.created_at * 1000,
        updated_at: (task.updated_at || task.created_at) * 1000,
      },
    };
  },
  pathTextToVideo: decodePathSubmit(TEXT_TO_VIDEO),
  pathImageToVideo: decodePathSubmit(IMAGE_TO_VIDEO),
  pathOmniVideo: decodePathSubmit(OMNI_VIDEO),
  pathTaskCreated: function (ctx, task) {
    const result = plainObject(task.data);
    return Object.assign({}, result, { data: Object.assign({}, plainObject(result.data), { id: task.task_id }) });
  },
  // The model-in-path API answers GET /tasks with a list; the gateway answers
  // for one task, from Kling's last reply. A task the gateway failed reads
  // failed.
  pathTasks: function (ctx, task) {
    const statusMap = {
      NOT_START: "submitted",
      SUBMITTED: "submitted",
      QUEUED: "submitted",
      IN_PROGRESS: "processing",
      SUCCESS: "succeeded",
      FAILURE: "failed",
    };
    const result = isPathReply(task.data) ? plainObject(task.data) : {};
    const entry = Object.assign(
      { status: statusMap[task.status] || "submitted", create_time: task.created_at * 1000, update_time: (task.updated_at || task.created_at) * 1000 },
      pathEntry(result),
      { id: task.task_id },
    );
    if (task.status === "FAILURE" && entry.status !== "failed") {
      entry.status = "failed";
      entry.message = task.fail_reason || "";
    }
    return Object.assign({ code: 0, message: "", request_id: "" }, result, { data: [entry] });
  },
  error: function (ctx, error) {
    return { code: ERROR_CODES[error.httpStatus] || (error.httpStatus >= 500 ? 5000 : 1200), message: error.message, request_id: error.requestId || "" };
  },
};

// Native routes send the client's own body with the channel's model. The host
// protocols add the prompt, first frame, mode and the aspect ratio nearest the
// size; image-to-video has no aspect ratio. They always send mode and
// duration, at Kling's defaults when absent.
export function buildSubmitRequest(ctx) {
  const req = ctx.requestBody;
  if (usesModelPath(ctx, req)) {
    const model = submitModel(ctx, req);
    const submission = pathSubmission(ctx);
    return {
      url: apiRoot(ctx) + "/" + submission.endpoint + "/" + model,
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, requestHeaders(ctx, true)),
      body: submission.body,
      action: PATH_ACTIONS[submission.endpoint],
    };
  }
  const metadata = req.metadata || {};
  const inferredAction = req.image || metadata.image || metadata.image_tail ? "image_to_video" : "text_to_video";
  const action = ctx.action === "text_to_video" || ctx.action === "image_to_video" ? ctx.action : inferredAction;
  const model = submitModel(ctx, req);
  if (!model) throw new Error("model_name is required");
  let body;
  if (req.vendorBody) body = Object.assign({}, req.vendorBody, { model_name: model });
  else {
    body = {};
    if (req.prompt) body.prompt = req.prompt;
    if (req.image) body.image = req.image;
    if (req.mode) body.mode = req.mode;
    if (action === "text_to_video" && req.size !== undefined && req.size !== null && req.size !== "") body.aspect_ratio = aspectRatio(sizeDimensions(req.size), KLING_ASPECT_RATIOS);
    Object.assign(body, metadata);
    body.model_name = model;
    body.mode = klingMode(body.mode);
    body.duration = String(outboundDuration(req));
    if (body.image) body.image = filePlaceholder(body.image);
    if (body.image_tail) body.image_tail = filePlaceholder(body.image_tail);
    if (!body.prompt) delete body.prompt;
    if (!body.image) delete body.image;
  }
  return {
    url: urlFor(ctx, action),
    method: "POST",
    headers: Object.assign({ "Content-Type": "application/json" }, requestHeaders(ctx)),
    body: body,
    action: action,
  };
}

// A model-in-path task keeps the resolution and, when HDR, the dynamic range it
// was billed at, for a settlement without a unit deduction; the state also
// marks the task as one.
function isPathTask(ctx) {
  return Boolean(plainObject(ctx && ctx.state).resolution);
}

export function parseSubmitResponse(ctx, resp) {
  const result = resp.body;
  if (!result || typeof result !== "object" || Array.isArray(result)) {
    // A page instead of Kling's JSON is usually a Base URL that does not reach
    // Kling's API.
    const header = plainObject(resp.headers)["Content-Type"];
    const contentType = trimmed(Array.isArray(header) ? header[0] : header);
    throw new Error(
      "kling submit failed: the upstream answered HTTP " +
        resp.statusCode +
        (contentType ? " " + contentType : "") +
        " instead of Kling's JSON; check the channel Base URL"
    );
  }
  if (result.code !== 0) throw new Error(result.message || "kling submit failed");
  const req = (ctx && ctx.requestBody) || {};
  if (usesModelPath(ctx, req)) {
    if (!result.data || !result.data.id) throw new Error("missing task id");
    const settings = plainObject(pathSubmission(ctx).body.settings);
    const state = { resolution: billedResolution(pathSpec(submitModel(ctx, req)), settings) };
    if (billedDynamicRange(settings) === "hdr") state.dynamicRange = "hdr";
    return { taskId: result.data.id, taskData: result, state: state };
  }
  if (!result.data || !result.data.task_id) throw new Error("missing task_id");
  return { taskId: result.data.task_id, taskData: result };
}

export function extractUsage(ctx) {
  if (ctx.usagePurpose === "billing_ratios") return null;
  const req = ctx.requestBody || {};
  if (usesModelPath(ctx, req)) return { units: pathReservedUnits(pathSpec(submitModel(ctx, req)), pathSubmission(ctx).body) };
  // A native body reads like the metadata of a host-protocol request.
  const source = req.vendorBody ? { metadata: req.vendorBody } : req;
  return { units: unitsPerSecond(submitModel(ctx, req), outboundMode(source), source.metadata || {}) * outboundDuration(source) };
}

export function buildQueryRequest(ctx) {
  const url = isPathTask(ctx) ? apiRoot(ctx) + "/tasks?task_ids=" + encodeURIComponent(ctx.taskId) : urlFor(ctx, ctx.action) + "/" + ctx.taskId;
  return { url: url, method: "GET", headers: requestHeaders(ctx, isPathTask(ctx)) };
}

function parsePathResult(ctx, body) {
  const entry = pathEntry(body, ctx.taskId);
  const statuses = { submitted: "SUBMITTED", processing: "IN_PROGRESS", succeeded: "SUCCESS", failed: "FAILURE" };
  const status = statuses[entry.status];
  if (!status) return { status: "UNKNOWN", reason: "unknown task status: " + String(entry.status || "") };
  const result = { code: body.code || 0, taskId: entry.id, status: status, reason: entry.message || "" };
  const videos = status === "SUCCESS" ? pathVideos(entry) : [];
  if (videos.length && videos[0].url) result.url = videos[0].url;
  const usage = pathCompletedUnits(ctx, body);
  if (usage && usage.units > 0) {
    result.completionTokens = Math.ceil(usage.units);
    result.totalTokens = Math.ceil(usage.units);
  }
  return result;
}

export function parseTaskResult(ctx, body) {
  if (isPathTask(ctx)) return parsePathResult(ctx, body);
  const data = body.data || {};
  const statuses = { submitted: "SUBMITTED", processing: "IN_PROGRESS", succeed: "SUCCESS", failed: "FAILURE" };
  const status = statuses[data.task_status];
  if (!status) return { status: "UNKNOWN", reason: "unknown task status: " + String(data.task_status || "") };
  const videos = status === "SUCCESS" && data.task_result && data.task_result.videos ? data.task_result.videos : [];
  const result = { code: body.code || 0, taskId: data.task_id, status: status, reason: data.task_status_msg || "" };
  if (videos.length && videos[0].url) result.url = videos[0].url;
  const units = Number.parseFloat(data.final_unit_deduction || "");
  if (Number.isFinite(units) && units > 0) {
    result.completionTokens = Math.ceil(units);
    result.totalTokens = Math.ceil(units);
  }
  return result;
}

function artifactData(ctx) {
  const data = (ctx && ctx.data) || {};
  if (data.data && typeof data.data === "object" && data.data.task_id && Object.prototype.hasOwnProperty.call(data.data, "data")) return data.data.data || {};
  return data;
}

function artifactVideoURL(ctx) {
  if (isPathTask(ctx)) {
    const videos = pathVideos(pathEntry(artifactData(ctx), ctx.taskId));
    return videos.length ? String(videos[0].url || "").trim() : "";
  }
  const result = (artifactData(ctx).data || {}).task_result || {};
  const videos = Array.isArray(result.videos) ? result.videos : [];
  return videos.length ? String(videos[0].url || "").trim() : "";
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" && artifactVideoURL(task) ? [{ key: "video", type: "video" }] : [];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  const url = artifactVideoURL(ctx);
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: ctx.clientRequest.method, credentialless: true };
}

export function extractUsageOnComplete(ctx, _taskResult, body) {
  if (isPathTask(ctx)) return pathCompletedUnits(ctx, body);
  const data = (body && body.data) || {};
  if (
    !Object.prototype.hasOwnProperty.call(data, "final_unit_deduction") ||
    data.final_unit_deduction === undefined ||
    data.final_unit_deduction === null ||
    data.final_unit_deduction === ""
  ) {
    return null;
  }
  const units = Number.parseFloat(data.final_unit_deduction);
  if (!Number.isFinite(units)) return null;
  return { units: units };
}

export const protocols = {
  openai_responses: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      const model = trimmed(ctx.model);
      if (!model) throw new Error("model is required");
      if (req.input !== undefined && typeof req.input !== "string" && !Array.isArray(req.input)) throw new Error("input must be a string or array");
      if (req.images !== undefined && !Array.isArray(req.images)) throw new Error("images must be an array");
      if (req.metadata !== undefined && (!req.metadata || typeof req.metadata !== "object" || Array.isArray(req.metadata)))
        throw new Error("metadata must be an object");
      const input = responsesInput(req);
      const prompt = input.prompt || trimmed(req.prompt);
      const images = [];
      for (const image of [req.image, req.input_reference].concat(req.images || [], input.images)) {
        if (trimmed(image) && !images.includes(trimmed(image))) images.push(trimmed(image));
      }
      if (!prompt && images.length === 0) throw new Error("input is required");
      const metadata = Object.assign({}, req.metadata || {});
      if (Object.prototype.hasOwnProperty.call(req, "mode")) metadata.mode = req.mode;
      metadata.mode = klingMode(metadata.mode);
      if (images.length > 1 && !metadata.image_tail) metadata.image_tail = images[1];
      const requestBody = { model: model, prompt: prompt, metadata: metadata };
      if (images.length) requestBody.image = images[0];
      if (Object.prototype.hasOwnProperty.call(req, "seconds")) requestBody.duration = req.seconds;
      else if (Object.prototype.hasOwnProperty.call(req, "duration")) requestBody.duration = req.duration;
      if (Object.prototype.hasOwnProperty.call(req, "size")) requestBody.size = req.size;
      return { kind: "submit", model: model, action: images.length ? "image_to_video" : "text_to_video", requestBody: requestBody };
    },
    renderEvents: function (ctx, task, previousState) {
      const status = String(task.status || "UNKNOWN").toUpperCase();
      const value = Number(String(task.progress || "").replace("%", ""));
      const progress = Number.isFinite(value) && value >= 0 && value <= 100 ? value : null;
      const state = { status: status, progress: progress };
      if (status === "SUCCESS") {
        const text = responsesVideoText(ctx);
        const events = previousState && previousState.status === status ? [] : [{ type: "output", data: text }];
        return { events: events, state: state, done: true };
      }
      if (status === "FAILURE")
        return { events: [{ type: "error", code: "task_failed", message: task.fail_reason || "task failed" }], state: state, done: true };
      if (previousState && previousState.status === status && previousState.progress === progress) return { events: [], state: state, done: false };
      const event = { type: "progress", message: status.toLowerCase() };
      if (progress !== null) event.progress = progress;
      return { events: [event], state: state, done: false };
    },
    renderFinal: function (ctx, _task) {
      return {
        output: [
          {
            type: "message",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: responsesVideoText(ctx), annotations: [], logprobs: [] }],
          },
        ],
        metadata: { vendor: "kling" },
      };
    },
  },
  openai_video: {
    decodeRequest: function (ctx) {
      if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
      let req;
      let hasInputReferenceFile = false;
      if (ctx.body.kind === "json") {
        if (!ctx.body.value || Array.isArray(ctx.body.value)) throw new Error("JSON object required");
        req = Object.assign({}, ctx.body.value);
      } else {
        const first = function (name) {
          const values = (ctx.body.fields || {})[name] || [];
          if (values.length > 1) throw new Error(name + " must be provided once");
          return values[0];
        };
        req = {};
        const fields = ctx.body.fields || {};
        for (const name of Object.keys(fields)) {
          req[name] = first(name);
        }
        for (const file of ctx.body.files || []) {
          if (file.field !== "input_reference") throw new Error("unexpected file field: " + file.field);
          if (hasInputReferenceFile) throw new Error("input_reference must be provided once");
          hasInputReferenceFile = true;
        }
        if (req.metadata !== undefined) {
          let parsed;
          try {
            parsed = JSON.parse(req.metadata);
          } catch (e) {
            throw new Error("metadata must be a JSON object string");
          }
          if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("metadata must be a JSON object string");
          req.metadata = parsed;
        }
        if (req.seconds !== undefined) req.seconds = Number(req.seconds);
        else if (req.duration !== undefined) req.seconds = Number(req.duration);
      }
      const seconds = req.seconds === undefined ? req.duration : req.seconds;
      // Without a duration the request builder sends Kling's default, 5, and
      // Kling 4.0 and 4.0 Flash pick the length themselves.
      if (seconds !== undefined) req.duration = validateKlingDuration(seconds);
      if (hasInputReferenceFile) {
        req.image = { __fileRef: "request_file:input_reference", encoding: "base64", maxBytes: 10485760 };
      } else {
        const image = trimmed(req.input_reference || req.image);
        if (image) req.image = image;
      }
      const metadata = req.metadata || {};
      req.mode = klingMode(req.mode || metadata.mode);
      const hasImage = hasKlingImage(req, hasInputReferenceFile);
      // vendorBody carries a native request's own body; clients of this protocol cannot set it.
      delete req.vendorBody;
      return {
        kind: "submit",
        model: ctx.model,
        action: hasImage ? "image_to_video" : "text_to_video",
        requestBody: Object.assign({}, req, { model: ctx.model }),
      };
    },
    render: function (ctx, task) {
      const response = task.data || {};
      const data = response.data || {};
      const statusMap = { NOT_START: "queued", SUBMITTED: "queued", QUEUED: "queued", IN_PROGRESS: "in_progress", SUCCESS: "completed", FAILURE: "failed" };
      const output = {
        id: task.task_id,
        object: "video",
        model: "",
        status: statusMap[task.status] || "unknown",
        progress: Number(String(task.progress || "0").replace("%", "")),
        created_at: data.created_at || 0,
      };
      // Model-in-path replies carry the task as data, or as the only entry of
      // the list GET /tasks answers.
      if (isPathReply(response)) {
        const entry = pathEntry(response);
        const videos = pathVideos(entry);
        output.created_at = entry.create_time || 0;
        if (entry.update_time) output.completed_at = entry.update_time;
        if (videos.length && videos[0].duration) output.seconds = videos[0].duration;
        if (response.code !== 0 && response.message) output.error = { message: response.message, code: String(response.code) };
        if (entry.status === "failed") output.error = { message: entry.message || "", code: "" };
        return output;
      }
      if (data.updated_at) output.completed_at = data.updated_at;
      const videos = data.task_result && data.task_result.videos ? data.task_result.videos : [];
      if (videos.length) {
        if (videos[0].duration) output.seconds = videos[0].duration;
      }
      if (response.code !== 0 && response.message) output.error = { message: response.message, code: String(response.code) };
      if (data.task_status === "failed") output.error = { message: data.task_status_msg, code: "" };
      return output;
    },
  },
};
