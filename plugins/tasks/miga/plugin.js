// MAGI-2 video generation through the DMC API (api.ll2.cn).
// Native routes mirror DMC's own paths, so clients use DMC's documented
// request and response shapes with our gateway address and API key:
//   POST /miga/v2/generations            create
//   GET  /miga/v2/generations/:task_id   query
// The query answers output.videoUrl with the gateway's signed artifact URL,
// which downloads without credentials.

const MODEL = "magi-2";
const MIN_DURATION = 4;
const MAX_DURATION = 30;
const ASPECT_RATIOS = ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"];
const RESOLUTIONS = ["1080p"];
const MAX_PARTS = 21;
const MAX_TEXT_CHARS = 15000;
const MAX_URL_CHARS = 8192;
const PART_LIMITS = { reference_image: 10, reference_video: 5, reference_audio: 5 };
const PART_MEDIA_FIELD = { reference_image: "image", reference_video: "video", reference_audio: "audio" };

export const meta = {
  apiVersion: 1,
  key: "miga",
  name: "MAGI",
  icon: "text:MG",
  description: {
    en: "MAGI-2 video generation via DMC from text with image, video or audio references",
    zh: "通过 DMC 调用 MAGI-2 视频生成：文生视频，可加入图片、视频、音频参考",
  },
  version: "1.0.1",
  author: { name: "openai-next" },
  baseUrl: "https://api.ll2.cn",
  // DMC's signed artifact links are served from hub.ll2.cn.
  allowedHosts: ["hub.ll2.cn"],
  models: [MODEL],
  fetchMode: "per_task",
  usageSchema: {
    seconds: {
      type: "number",
      unit: "second",
      description: { en: "Video generation unit price", zh: "视频生成单价" },
    },
  },
  usageExamples: [
    { label: "magi-2 5s", facts: { seconds: 5 } },
    { label: "magi-2 10s", facts: { seconds: 10 } },
    { label: "magi-2 30s", facts: { seconds: 30 } },
  ],
  routes: [
    { method: "POST", path: "/miga/v2/generations", type: "submit", decode: "createGeneration", render: "generationCreated", models: [MODEL] },
    { method: "GET", path: "/miga/v2/generations/:task_id", type: "query", render: "generationStatus" },
  ],
};

function trimmed(value) {
  return String(value === undefined || value === null ? "" : value).trim();
}

function plainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : null;
}

function httpsURL(value, label) {
  if (typeof value !== "string" || !value) throw new Error(label + " is required");
  const url = value;
  if (url.length > MAX_URL_CHARS) throw new Error(label + " must be at most " + MAX_URL_CHARS + " characters");
  if (!/^https:\/\/[^\s]+$/i.test(url)) throw new Error(label + " must be an https URL");
  return url;
}

// Mirrors DMC's documented constraints so an invalid request is refused here,
// before anything is reserved or sent upstream. Values are checked with the
// JSON types DMC takes, because the body is forwarded as the client sent it.
// The model is not checked: the route admits magi-2, its case variants and a
// channel alias, and the body sent upstream carries the mapped name.
function validateGeneration(body) {
  const parts = body.parts;
  if (!Array.isArray(parts) || parts.length < 1 || parts.length > MAX_PARTS) {
    throw new Error("parts must be an array of 1 to " + MAX_PARTS + " items");
  }
  const counts = { text: 0, reference_image: 0, reference_video: 0, reference_audio: 0 };
  for (let i = 0; i < parts.length; i++) {
    const part = plainObject(parts[i]);
    if (!part) throw new Error("parts[" + i + "] must be an object");
    const type = part.type;
    if (type === "text") {
      if (typeof part.text !== "string" || !part.text.trim()) throw new Error("parts[" + i + "].text must be a non-empty string");
      if (part.text.length > MAX_TEXT_CHARS) throw new Error("text must be at most " + MAX_TEXT_CHARS + " characters");
      counts.text++;
    } else if (Object.prototype.hasOwnProperty.call(PART_LIMITS, type)) {
      const field = PART_MEDIA_FIELD[type];
      const media = plainObject(part[field]);
      httpsURL(media && media.url, "parts[" + i + "]." + field + ".url");
      counts[type]++;
      if (counts[type] > PART_LIMITS[type]) throw new Error("at most " + PART_LIMITS[type] + " " + type + " parts are allowed");
    } else {
      throw new Error("parts[" + i + "].type must be text, reference_image, reference_video or reference_audio");
    }
  }
  if (counts.text !== 1) throw new Error("parts must contain exactly one text item");
  const duration = body.duration;
  if (!Number.isInteger(duration) || duration < MIN_DURATION || duration > MAX_DURATION) {
    throw new Error("duration must be an integer from " + MIN_DURATION + " to " + MAX_DURATION + " seconds");
  }
  if (ASPECT_RATIOS.indexOf(body.aspectRatio) < 0) {
    throw new Error("aspectRatio must be one of " + ASPECT_RATIOS.join(", "));
  }
  if (body.resolution !== undefined && body.resolution !== null && RESOLUTIONS.indexOf(body.resolution) < 0) {
    throw new Error("resolution must be " + RESOLUTIONS.join(" or "));
  }
  if (body.generateAudio !== undefined && typeof body.generateAudio !== "boolean") throw new Error("generateAudio must be a boolean");
  if (body.seed !== undefined && body.seed !== null && !Number.isInteger(body.seed)) throw new Error("seed must be an integer");
  const hasReference = counts.reference_image + counts.reference_video + counts.reference_audio > 0;
  return { duration: duration, action: hasReference ? "image_to_video" : "text_to_video" };
}

function headerValue(headers, name) {
  const wanted = name.toLowerCase();
  for (const key of Object.keys(headers || {})) {
    if (key.toLowerCase() === wanted) return trimmed(headers[key]);
  }
  return "";
}

// The latest DMC query snapshot; before the first poll it is the create reply {id}.
function snapshot(task) {
  return plainObject(task && task.data) || {};
}

export const native = {
  createGeneration: function (ctx) {
    if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
    const body = plainObject(ctx.body.value);
    if (!body) throw new Error("request body must be a JSON object");
    if (typeof body.model !== "string" || !body.model.trim()) throw new Error("model must be " + MODEL);
    const checked = validateGeneration(body);
    return { kind: "submit", model: trimmed(body.model), action: checked.action, requestBody: { model: trimmed(body.model), vendorBody: body } };
  },
  generationCreated: function (ctx, task) {
    return { id: task.task_id };
  },
  generationStatus: function (ctx, task) {
    const data = snapshot(task);
    const out = {
      id: task.task_id,
      status: trimmed(data.status) || (task.status === "IN_PROGRESS" ? "processing" : "queued"),
      output: plainObject(data.output) ? Object.assign({}, data.output) : { videoUrl: null, urlExpiresAt: null },
      error: data.error === undefined ? null : data.error,
      createdAt: data.createdAt || null,
    };
    if (task.status === "SUCCESS") {
      const video = (ctx.artifacts || {}).video;
      // A signed gateway address: downloads without credentials.
      out.output.videoUrl = video && video.url ? video.url : "/v1/tasks/" + task.task_id + "/artifacts/video/content";
      out.output.urlExpiresAt = null;
      out.status = "succeeded";
    } else {
      out.output.videoUrl = null;
      // DMC may report succeeded before it hands out the video; the task is
      // still polled then, and a client reads succeeded as "download now".
      if (out.status === "succeeded") out.status = "processing";
    }
    if (task.status === "FAILURE" && out.status !== "failed" && out.status !== "canceled") {
      out.status = "failed";
      out.error = { code: "task_failed", message: task.fail_reason || "task failed" };
    }
    return out;
  },
  // DMC's error envelope: {code, message}.
  error: function (ctx, error) {
    return { code: error.code || (error.httpStatus >= 500 ? "server_error" : "invalid_request"), message: error.message };
  },
};

export function buildSubmitRequest(ctx) {
  const req = ctx.requestBody || {};
  const body = Object.assign({}, req.vendorBody || {}, { model: ctx.upstreamModel || MODEL });
  const checked = validateGeneration(body);
  const headers = { "Content-Type": "application/json", Accept: "application/json", Authorization: "Bearer " + ctx.apiKey };
  const idem = headerValue(ctx.requestHeaders, "Idempotency-Key");
  if (idem) headers["Idempotency-Key"] = idem;
  return {
    url: trimmed(ctx.baseUrl).replace(/\/+$/, "") + "/miga/v2/generations",
    method: "POST",
    headers: headers,
    body: body,
    action: checked.action,
  };
}

export function parseSubmitResponse(ctx, resp) {
  const body = plainObject(resp.body) || {};
  const id = trimmed(body.id);
  if (!id) throw new Error(trimmed(body.message) || "DMC create returned no task id");
  return { taskId: id, taskData: body };
}

export function extractUsage(ctx) {
  if (ctx.usagePurpose === "billing_ratios") return null;
  const req = ctx.requestBody || {};
  const body = req.vendorBody || {};
  return { seconds: Number(body.duration) };
}

export function buildQueryRequest(ctx) {
  return {
    url: trimmed(ctx.baseUrl).replace(/\/+$/, "") + "/miga/v2/generations/" + encodeURIComponent(ctx.taskId),
    method: "GET",
    headers: { Accept: "application/json", Authorization: "Bearer " + ctx.apiKey },
  };
}

export function parseTaskResult(ctx, body) {
  const data = plainObject(body) || {};
  const statuses = { queued: "QUEUED", processing: "IN_PROGRESS", succeeded: "SUCCESS", failed: "FAILURE", canceled: "FAILURE" };
  const status = statuses[trimmed(data.status)];
  if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(data.status || data.code) };
  const result = { code: 0, status: status, progress: status === "QUEUED" ? "20%" : status === "IN_PROGRESS" ? "50%" : "100%" };
  if (status === "SUCCESS" && !trimmed(data.output && data.output.videoUrl)) {
    // DMC reports no downloadable video yet; keep polling.
    return { code: 0, status: "IN_PROGRESS", progress: "90%" };
  }
  if (status === "FAILURE") {
    const err = plainObject(data.error) || {};
    result.reason = trimmed(err.message) || trimmed(err.code) || "task " + trimmed(data.status);
  }
  return result;
}

export function listArtifacts(task) {
  if (task.status !== "SUCCESS") return [];
  const output = plainObject(snapshot(task).output) || {};
  return trimmed(output.videoUrl) ? [{ key: "video", type: "video", mimeType: "video/mp4" }] : [];
}

// DMC serves the video on its own artifact route with the channel key.
export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  return {
    url: trimmed(ctx.baseUrl).replace(/\/+$/, "") + "/v1/tasks/" + encodeURIComponent(ctx.upstreamTaskId) + "/artifacts/video/content",
    method: ctx.clientRequest && ctx.clientRequest.method === "HEAD" ? "HEAD" : "GET",
    headers: { Authorization: "Bearer " + ctx.apiKey },
  };
}
