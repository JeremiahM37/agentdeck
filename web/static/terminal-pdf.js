// Loaded only when a PDF is opened; all rendering stays in this browser.
import * as pdfjs from "/vendor/pdf.mjs";
pdfjs.GlobalWorkerOptions.workerSrc = "/vendor/pdf.worker.mjs";
export async function renderPDF(blob, container) {
  const task = pdfjs.getDocument({
    data: new Uint8Array(await blob.arrayBuffer()),
    standardFontDataUrl: "/vendor/standard_fonts/",
    cMapUrl: "/vendor/cmaps/",
    cMapPacked: true,
    isEvalSupported: false,
  });
  const pdf = await task.promise;
  if (!container.isConnected) {
    pdf.destroy();
    return () => {};
  }
  let pageNumber = 1,
    busy = false,
    closed = false;
  const tools = document.createElement("div");
  tools.className = "pdf-controls";
  const previous = document.createElement("button");
  previous.textContent = "Previous page";
  const next = document.createElement("button");
  next.textContent = "Next page";
  const label = document.createElement("span");
  label.id = "pdf-page";
  const canvas = document.createElement("canvas");
  canvas.setAttribute("aria-label", "PDF page");
  tools.append(previous, label, next);
  container.replaceChildren(tools, canvas);
  async function draw(n) {
    if (busy || closed) return;
    busy = true;
    previous.disabled = next.disabled = true;
    try {
      const page = await pdf.getPage(n);
      if (closed) return;
      const original = page.getViewport({ scale: 1 });
      const width = Math.max(280, Math.min(1000, container.clientWidth || 800));
      const viewport = page.getViewport({ scale: width / original.width });
      const ratio = Math.min(devicePixelRatio || 1, 2);
      canvas.width = Math.floor(viewport.width * ratio);
      canvas.height = Math.floor(viewport.height * ratio);
      canvas.style.width = viewport.width + "px";
      await page.render({
        canvasContext: canvas.getContext("2d"),
        viewport,
        transform: [ratio, 0, 0, ratio, 0, 0],
      }).promise;
      label.textContent = `Page ${n} of ${pdf.numPages}`;
      pageNumber = n;
    } catch (e) {
      label.textContent = e.message;
    } finally {
      busy = false;
      previous.disabled = pageNumber <= 1;
      next.disabled = pageNumber >= pdf.numPages;
    }
  }
  previous.onclick = () => draw(pageNumber - 1);
  next.onclick = () => draw(pageNumber + 1);
  await draw(1);
  return () => {
    closed = true;
    pdf.destroy();
  };
}
