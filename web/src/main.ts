// UI for the assistant: streams Server-Sent Events from the Go API, shows the
// sources and, for the agent, the tool calls it made.
//
// EventSource cannot POST a JSON body, so the stream is read from fetch() and the
// SSE blocks are parsed by hand (event: / data: pairs separated by a blank line).

type Source = {
  source_path: string;
  heading_path: string;
  distance: number;
  content: string;
};

type ToolTrace = {
  iteration: number;
  name: string;
  arguments: string;
  result: string;
  error: string;
};

const $ = <T extends HTMLElement>(id: string): T => {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing element #${id}`);
  return el as T;
};

const askForm = $<HTMLFormElement>("ask-form");
const askInput = $<HTMLTextAreaElement>("ask-input");
const askMode = $<HTMLSelectElement>("ask-mode");
const askSubmit = $<HTMLButtonElement>("ask-submit");
const askCancel = $<HTMLButtonElement>("ask-cancel");
const askStatus = $<HTMLParagraphElement>("ask-status");
const askError = $<HTMLDivElement>("ask-error");
const askAnswer = $<HTMLElement>("ask-answer");
const askTimings = $<HTMLDivElement>("ask-timings");
const askSourcesWrap = $<HTMLElement>("ask-sources-wrap");
const askSources = $<HTMLUListElement>("ask-sources");

const agentForm = $<HTMLFormElement>("agent-form");
const agentInput = $<HTMLTextAreaElement>("agent-input");
const agentSubmit = $<HTMLButtonElement>("agent-submit");
const agentStatus = $<HTMLParagraphElement>("agent-status");
const agentError = $<HTMLDivElement>("agent-error");
const agentAnswer = $<HTMLElement>("agent-answer");
const agentToolsWrap = $<HTMLElement>("agent-tools-wrap");
const agentTools = $<HTMLOListElement>("agent-tools");
const stats = $<HTMLParagraphElement>("stats");

let controller: AbortController | null = null;

/** streamSSE posts a JSON body and calls onEvent for every parsed SSE block. */
async function streamSSE(
  path: string,
  body: unknown,
  onEvent: (event: string, data: any) => void,
  signal: AbortSignal,
): Promise<void> {
  const response = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });

  if (!response.ok || !response.body) {
    const detail = await response.text().catch(() => "");
    throw new Error(`HTTP ${response.status}. ${detail}`.trim());
  }

  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += value;

    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);

      let event = "message";
      let data = "";
      for (const line of block.split("\n")) {
        if (line.startsWith("event:")) event = line.slice(6).trim();
        else if (line.startsWith("data:")) data += line.slice(5).trim();
      }
      if (data) onEvent(event, JSON.parse(data));

      boundary = buffer.indexOf("\n\n");
    }
  }
}

function setBusy(section: "ask" | "agent", busy: boolean, label: string): void {
  const submit = section === "ask" ? askSubmit : agentSubmit;
  const status = section === "ask" ? askStatus : agentStatus;
  submit.disabled = busy;
  status.textContent = label;
  if (section === "ask") {
    askAnswer.setAttribute("aria-busy", String(busy));
    askCancel.hidden = !busy;
  } else {
    agentAnswer.setAttribute("aria-busy", String(busy));
  }
}

function showError(target: HTMLDivElement, message: string): void {
  target.textContent = message;
  target.hidden = message === "";
}

function appendText(target: HTMLElement, text: string): void {
  target.textContent = (target.textContent ?? "") + text;
}

function renderSources(list: Source[]): void {
  askSources.replaceChildren();
  askSourcesWrap.hidden = list.length === 0;
  if (list.length === 0) return;

  for (const [index, source] of list.entries()) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source";
    button.setAttribute("aria-expanded", "false");
    button.textContent = `${index + 1}. ${source.source_path} > ${source.heading_path} (${source.distance.toFixed(3)})`;

    const fragment = document.createElement("pre");
    fragment.hidden = true;
    fragment.textContent = source.content;
    button.setAttribute("aria-controls", `fragment-${index}`);
    fragment.id = `fragment-${index}`;

    button.addEventListener("click", () => {
      const expanded = button.getAttribute("aria-expanded") === "true";
      button.setAttribute("aria-expanded", String(!expanded));
      fragment.hidden = expanded;
    });

    item.append(button, fragment);
    askSources.append(item);
  }
}

/** pretty formats JSON results; anything that is not JSON is shown as-is. */
function pretty(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

function compact(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value));
  } catch {
    return value;
  }
}

function renderTools(traces: ToolTrace[]): void {
  agentTools.replaceChildren();
  agentToolsWrap.hidden = traces.length === 0;

  for (const trace of traces) {
    const item = document.createElement("li");
    const title = document.createElement("code");
    title.textContent = `[${trace.iteration}] ${trace.name}(${compact(trace.arguments)})`;
    const outcome = document.createElement("p");
    outcome.className = trace.error ? "tool-error" : "tool-result";
    outcome.textContent = trace.error ? `ERROR: ${trace.error}` : pretty(trace.result);
    item.append(title, outcome);
    agentTools.append(item);
  }
}

askForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const question = askInput.value.trim();
  if (!question) return;

  controller?.abort();
  controller = new AbortController();

  askAnswer.textContent = "";
  askTimings.hidden = true;
  renderSources([]);
  showError(askError, "");
  setBusy("ask", true, "Consultando…");

  try {
    await streamSSE(
      "/api/ask",
      { question, mode: askMode.value },
      (name, data) => {
        if (name === "delta") appendText(askAnswer, data.text as string);
        else if (name === "sources") renderSources((data.sources ?? []) as Source[]);
        else if (name === "done") {
          askTimings.hidden = false;
          askTimings.textContent = `recuperación ${data.retrieval_ms} ms · generación ${data.generate_ms} ms`;
        } else if (name === "agentic") {
          askTimings.hidden = false;
          askTimings.textContent = `intentos ${data.attempts} · consultas: ${(data.queries as string[]).join(" | ")} · juez: ${data.reason}`;
        } else if (name === "error") {
          showError(askError, data.message as string);
        }
      },
      controller.signal,
    );
    setBusy("ask", false, "Listo.");
  } catch (error) {
    if ((error as Error).name === "AbortError") {
      setBusy("ask", false, "Cancelado.");
    } else {
      setBusy("ask", false, "Falló.");
      showError(askError, (error as Error).message);
    }
  }
});

askCancel.addEventListener("click", () => controller?.abort());

agentForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const prompt = agentInput.value.trim();
  if (!prompt) return;

  controller?.abort();
  controller = new AbortController();

  agentAnswer.textContent = "";
  renderTools([]);
  showError(agentError, "");
  setBusy("agent", true, "Ejecutando…");

  try {
    await streamSSE(
      "/api/agent",
      { prompt },
      (name, data) => {
        if (name === "delta") appendText(agentAnswer, data.text as string);
        else if (name === "tools") renderTools((data.tools ?? []) as ToolTrace[]);
        else if (name === "done") setBusy("agent", false, `Listo en ${data.iterations} iteraciones.`);
        else if (name === "error") showError(agentError, data.message as string);
      },
      controller.signal,
    );
    if (agentStatus.textContent === "Ejecutando…") setBusy("agent", false, "Listo.");
  } catch (error) {
    if ((error as Error).name === "AbortError") {
      setBusy("agent", false, "Cancelado.");
    } else {
      setBusy("agent", false, "Falló.");
      showError(agentError, (error as Error).message);
    }
  }
});

// Ctrl+Enter submits from either textarea.
for (const textarea of [askInput, agentInput]) {
  textarea.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      textarea.form?.requestSubmit();
    }
  });
}

fetch("/api/stats")
  .then((response) => (response.ok ? response.json() : null))
  .then((data) => {
    if (data) stats.textContent = `${data.documents} documentos · ${data.chunks} fragmentos indexados`;
  })
  .catch(() => {
    stats.textContent = "no se pudo leer el estado del índice";
  });
