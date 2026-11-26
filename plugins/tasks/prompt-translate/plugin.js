// prompt-translate translates the Chinese of a new user prompt into English,
// through a gateway channel, before the prompt goes upstream. It works only
// where a channel binds it: the hook then runs on each attempt's upstream
// body. Every error throws, and with optional set the host then sends the
// prompt as it was.
//
// Only the request that brings a new prompt is translated. The client keeps
// its own history and sends the original text again with the requests that
// follow, so the upstream prompt cache breaks only at that one message.

const TRANSLATE_PROMPT = [
  "You translate messages for a coding assistant.",
  "The user message is a JSON array of strings.",
  "Translate the Chinese in each string into natural English.",
  "Keep code, identifiers, file paths, commands, URLs, numbers, Markdown and line breaks exactly as written, and leave text that is already in English unchanged.",
  "Do not answer, explain or add anything.",
  "Reply with only a JSON array of the translated strings, with the same number of items in the same order.",
].join(" ");

// Appended to the translated prompt when replies stay in Chinese.
const REPLY_NOTE = "\n\n(Please reply in Chinese.)";

export const meta = {
  apiVersion: 1,
  key: "prompt-translate",
  name: { en: "Prompt translation", zh: "提问翻译" },
  description: {
    en: "Translates the Chinese in new user prompts into English before they go upstream",
    zh: "发往上游前把用户新提问里的中文翻成英文",
  },
  version: "1.0.0",
  author: { name: "Shimmer Technology" },
  optional: true,
  configSchema: {
    type: "object",
    properties: {
      url: {
        type: "string",
        format: "url",
        title: { en: "Chat Completions URL", zh: "对话接口地址" },
        description: {
          en: "Chat Completions endpoint of the gateway that serves the translation channel",
          zh: "提供翻译渠道的网关的对话接口",
        },
        default: "http://127.0.0.1:3000/v1/chat/completions",
        minLength: 1,
        maxLength: 2048,
        examples: ["http://127.0.0.1:3000/v1/chat/completions"],
      },
      apiKey: {
        type: "string",
        format: "password",
        title: { en: "API key", zh: "API 密钥" },
        description: {
          en: "Token that reaches only the translation channel, such as an admin token ending in -<channel id>",
          zh: "只能用到翻译渠道的令牌，例如以 -渠道ID 结尾的管理员令牌",
        },
        default: "",
        maxLength: 1024,
        examples: ["sk-...-13"],
      },
      model: {
        type: "string",
        title: { en: "Translation model", zh: "翻译模型" },
        description: { en: "Model name the translation channel serves", zh: "翻译渠道提供的模型名" },
        default: "",
        maxLength: 128,
        examples: ["gpt-4.1-mini"],
      },
      maxChars: {
        type: "integer",
        title: { en: "Longest text translated", zh: "最长翻译文本" },
        description: {
          en: "A prompt with more text to translate than this goes as it is",
          zh: "要翻译的文字超过这个长度时按原文发送",
        },
        unit: "character",
        default: 2000,
        minimum: 100,
        maximum: 20000,
      },
      replyInChinese: {
        type: "boolean",
        title: { en: "Keep replies in Chinese", zh: "保持中文回复" },
        description: {
          en: "Adds an English line asking the model to reply in Chinese",
          zh: "在译文后加一句英文，让模型继续用中文回答",
        },
        default: true,
      },
    },
  },
  samples: [
    {
      name: { en: "Async refactor", zh: "改成异步" },
      lang: "zh",
      format: "openai",
      body: {
        model: "gpt-4o-mini",
        messages: [{ role: "user", content: "帮我把 loadUser 改成异步函数，并加上错误处理。" }],
      },
    },
    {
      name: { en: "Fix with code", zh: "带代码的修复" },
      lang: "zh",
      format: "claude",
      body: {
        model: "claude-sonnet-4",
        max_tokens: 1024,
        messages: [
          {
            role: "user",
            content: [{ type: "text", text: "这段代码为什么报错？\n\n```js\nconst total = items.reduce((a, b) => a + b.price)\n```" }],
          },
        ],
      },
    },
  ],
};

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

// Text that coding tools add to a user message on their own: reminders,
// slash command echoes, shell output and environment notes. It goes as it
// is, as does fenced code.
const HARNESS_TAGS = [
  "system-reminder",
  "command-name",
  "command-message",
  "command-args",
  "local-command-caveat",
  "local-command-stdout",
  "local-command-stderr",
  "bash-input",
  "bash-stdout",
  "bash-stderr",
  "user-prompt-submit-hook",
  "environment_context",
  "user_instructions",
];
const KEPT = new RegExp(
  "```[\\s\\S]*?```|" + HARNESS_TAGS.map((tag) => "<" + tag + "(?:\\s[^>]*)?>[\\s\\S]*?</" + tag + ">").join("|"),
  "g",
);

const CJK = /[㐀-䶿一-鿿豈-﫿]/;
// A json.patch value must not hold a file reference.
const MEDIA = /\[media:\d+\]/;
const TEXT_TYPES = ["text", "input_text"];
// The most pieces of text one translation request carries.
const MAX_PIECES = 64;

function pushText(fields, path, value) {
  if (typeof value === "string" && CJK.test(value) && !MEDIA.test(value)) fields.push({ path: path, text: value });
}

// contentTexts adds the text parts of a message content: a string, or OpenAI,
// Responses, Claude or Gemini parts. Tool results, images and thoughts are
// left out.
function contentTexts(fields, path, content) {
  if (typeof content === "string") {
    pushText(fields, path, content);
    return;
  }
  if (!Array.isArray(content)) return;
  content.forEach((part, i) => {
    if (!isObject(part) || part.thought === true || typeof part.text !== "string") return;
    if (part.type === undefined || TEXT_TYPES.includes(part.type)) pushText(fields, path + "/" + i + "/text", part.text);
  });
}

// lastUserTexts returns the text fields with Chinese of the request's last
// message when that is a user message, with their JSON Pointers. A coding
// tool sends many requests for one prompt; all but the first end in a tool
// result, which gives none.
function lastUserTexts(format, body) {
  const fields = [];
  if (!isObject(body)) return fields;
  if (format === "openai" || format === "claude") {
    const messages = body.messages;
    if (!Array.isArray(messages) || messages.length === 0) return fields;
    const index = messages.length - 1;
    const last = messages[index];
    if (isObject(last) && last.role === "user") contentTexts(fields, "/messages/" + index + "/content", last.content);
  } else if (format === "openai_responses") {
    if (typeof body.input === "string") {
      pushText(fields, "/input", body.input);
      return fields;
    }
    if (!Array.isArray(body.input) || body.input.length === 0) return fields;
    const index = body.input.length - 1;
    const last = body.input[index];
    if (isObject(last) && last.role === "user" && (last.type === undefined || last.type === "message")) {
      contentTexts(fields, "/input/" + index + "/content", last.content);
    }
  } else if (format === "gemini") {
    if (!Array.isArray(body.contents) || body.contents.length === 0) return fields;
    const index = body.contents.length - 1;
    const last = body.contents[index];
    if (isObject(last) && (last.role === undefined || last.role === "user")) contentTexts(fields, "/contents/" + index + "/parts", last.parts);
  }
  return fields;
}

// segments splits text into the parts that go as they are, harness blocks
// and fenced code, and the parts between them.
function segments(text) {
  const parts = [];
  let end = 0;
  text.replace(KEPT, (match, offset) => {
    if (offset > end) parts.push({ text: text.slice(end, offset), kept: false });
    parts.push({ text: match, kept: true });
    end = offset + match.length;
    return match;
  });
  if (end < text.length) parts.push({ text: text.slice(end), kept: false });
  return parts;
}

// pinnedChannel is the channel an admin token ending in -<channel id> is
// pinned to, 0 for another token.
function pinnedChannel(apiKey) {
  const match = /-(\d+)$/.exec(apiKey.trim());
  return match ? Number(match[1]) : 0;
}

function configured(config) {
  return config.url !== "" && config.apiKey.trim() !== "" && config.model.trim() !== "";
}

// ownRequest reports the plugin's own call reaching it again: a request for
// the translation model, or one on the channel the token is pinned to.
function ownRequest(ctx, config) {
  if (ctx.model === config.model.trim()) return true;
  return isObject(ctx.channel) && ctx.channel.id === pinnedChannel(config.apiKey);
}

// translate sends the pieces to the translation model and returns their
// translations in order.
function translate(config, pieces, total) {
  const response = utils.fetch({
    url: config.url,
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: "Bearer " + config.apiKey.trim() },
    body: JSON.stringify({
      model: config.model.trim(),
      stream: false,
      max_tokens: Math.min(8192, 256 + total * 3),
      messages: [
        { role: "system", content: TRANSLATE_PROMPT },
        { role: "user", content: JSON.stringify(pieces) },
      ],
    }),
  });
  if (response.status < 200 || response.status > 299) throw new Error("translation API returned HTTP " + response.status);
  const choices = isObject(response.body) ? response.body.choices : undefined;
  const message = Array.isArray(choices) && isObject(choices[0]) ? choices[0].message : undefined;
  let content = isObject(message) ? message.content : undefined;
  if (Array.isArray(content)) content = content.map((part) => (isObject(part) && typeof part.text === "string" ? part.text : "")).join("");
  if (typeof content !== "string") throw new Error("translation API returned no message");
  const reply = content
    .replace(/<think>[\s\S]*?<\/think>/g, "")
    .trim()
    .replace(/^```(?:json)?\s*/, "")
    .replace(/\s*```$/, "");
  let translated;
  try {
    translated = JSON.parse(reply);
  } catch (error) {
    translated = undefined;
  }
  const valid =
    Array.isArray(translated) &&
    translated.length === pieces.length &&
    translated.every((text) => typeof text === "string" && text.trim() !== "" && !MEDIA.test(text));
  if (!valid) throw new Error("translation reply is not a JSON array with one string per piece");
  return translated.map((text) => text.trim());
}

export function ingress(ctx, req) {
  const config = ctx.config;
  // A request without a channel is at the ingress of a global or group
  // instance, which this plugin leaves alone. A dry run has no token either
  // and is translated.
  if ((!isObject(ctx.channel) && isObject(ctx.token)) || !configured(config) || ownRequest(ctx, config)) {
    return { action: "continue" };
  }
  const fields = [];
  const pieces = [];
  let total = 0;
  for (const field of lastUserTexts(ctx.format, req.body)) {
    const parts = segments(field.text);
    for (const part of parts) {
      if (part.kept || !CJK.test(part.text)) continue;
      const core = part.text.trim();
      part.lead = part.text.slice(0, part.text.indexOf(core));
      part.trail = part.text.slice(part.lead.length + core.length);
      part.piece = pieces.length;
      pieces.push(core);
      total += core.length;
    }
    fields.push({ path: field.path, parts: parts });
  }
  if (pieces.length === 0 || pieces.length > MAX_PIECES || total > config.maxChars) return { action: "continue" };

  const translated = translate(config, pieces, total);
  const ops = [];
  for (const field of fields) {
    if (!field.parts.some((part) => part.piece !== undefined)) continue;
    const text = field.parts.map((part) => (part.piece === undefined ? part.text : part.lead + translated[part.piece] + part.trail)).join("");
    ops.push({ op: "replace", path: field.path, value: text });
  }
  if (config.replyInChinese) ops[ops.length - 1].value += REPLY_NOTE;
  return { action: "continue", effects: [{ type: "json.patch", ops: ops }] };
}
