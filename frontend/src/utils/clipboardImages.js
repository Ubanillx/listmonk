// TinyMCE 5 ignores clipboard files alongside HTML and strips Word image tags.
// Resolve clipboard bytes first, then protect image positions during its paste cleanup.
const MAX_IMAGE_BYTES = 32 * 1024 * 1024;
const SOURCE_ATTRIBUTE = 'data-clipboard-image-source';

const readDataURL = (blob) => new Promise((resolve, reject) => {
  if (!blob.size || blob.size > MAX_IMAGE_BYTES) {
    reject(new Error('Clipboard image exceeds the upload limit'));
    return;
  }
  const reader = new FileReader();
  reader.onload = () => resolve(reader.result);
  reader.onerror = () => reject(reader.error);
  reader.readAsDataURL(blob);
});

const filename = (source) => {
  try {
    return decodeURIComponent(source.split(/[?#]/)[0].split(/[\\/]/).pop()).toLowerCase();
  } catch (error) {
    return '';
  }
};

const localSource = (source) => !source || /^(?:file:|cid:|webkit-fake-url:)/i.test(source);

// Word/WPS can put image bytes only in RTF. Read PNG/JPEG pict groups, including
// nested blip metadata, without interpreting RTF fields or accessing local paths.
const rtfImages = (rtf) => {
  const images = [];
  const starts = /\{\\pict\b/g;
  let match = starts.exec(rtf);
  while (match) {
    let depth = 1;
    let end = starts.lastIndex;
    while (end < rtf.length && depth > 0) {
      if (rtf[end] === '\\') end += 1;
      else if (rtf[end] === '{') depth += 1;
      else if (rtf[end] === '}') depth -= 1;
      end += 1;
    }
    if (depth !== 0) break;
    const group = rtf.slice(starts.lastIndex, end - 1);
    starts.lastIndex = end;
    let type = '';
    if (/\\pngblip\b/.test(group)) type = 'image/png';
    else if (/\\jpegblip\b/.test(group)) type = 'image/jpeg';
    const hex = group.replace(/\{[^{}]*\}/g, '').replace(/\\[a-z]+-?\d* ?/gi, '').replace(/\s/g, '');
    if (type && !/\\bin\d+/.test(group) && hex && hex.length % 2 === 0
      && hex.length <= MAX_IMAGE_BYTES * 2 && /^[\da-f]+$/i.test(hex)) {
      const bytes = new Uint8Array(hex.length / 2);
      for (let i = 0; i < bytes.length; i += 1) bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
      images.push(new Blob([bytes], { type }));
    }
    match = starts.exec(rtf);
  }
  return images;
};

const downloadImage = async (source, signal) => {
  const response = await fetch(source, { signal, credentials: 'omit', referrerPolicy: 'no-referrer' });
  if (!response.ok || !response.headers.get('content-type')?.startsWith('image/')) throw new Error('Image is unavailable');
  if (Number(response.headers.get('content-length')) > MAX_IMAGE_BYTES) throw new Error('Image is too large');
  const reader = response.body.getReader();
  const chunks = [];
  let length = 0;
  try {
    let chunk = await reader.read();
    while (!chunk.done) {
      length += chunk.value.length;
      if (length > MAX_IMAGE_BYTES) throw new Error('Image is too large');
      chunks.push(chunk.value);
      // eslint-disable-next-line no-await-in-loop
      chunk = await reader.read();
    }
  } finally {
    await reader.cancel();
  }
  return new Blob(chunks, { type: response.headers.get('content-type') });
};

export default function clipboardImages(editor, readMedia, onError) {
  let pending = Promise.resolve();
  let disposed = false;
  let pasteAsText = false;
  let shiftPaste = false;
  let sequence = 0;
  let protectedImages = new Map();
  const requests = new Set();

  const resolveSource = async (source) => {
    if (/^data:image\//i.test(source)) return source;
    if (localSource(source)) throw new Error('Clipboard does not contain image bytes');
    const url = new URL(source, window.location.href);
    if (url.origin === window.location.origin && /^\/api\/media\/file\//.test(url.pathname)) {
      return readDataURL(await readMedia(url.pathname));
    }
    if (!['http:', 'https:', 'blob:'].includes(url.protocol)) throw new Error('Unsupported image source');
    const controller = new AbortController();
    requests.add(controller);
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      return await readDataURL(await downloadImage(url.href, controller.signal));
    } finally {
      clearTimeout(timer);
      requests.delete(controller);
    }
  };

  const importImages = async (images, files = [], rtf = '') => {
    const sources = [...new Set(images.map((image) => image.getAttribute(SOURCE_ATTRIBUTE) || image.getAttribute('src') || ''))];
    const missing = sources.filter(localSource);
    const matches = new Map();
    const unusedFiles = new Set(files);
    missing.forEach((source) => {
      const file = files.find((candidate) => unusedFiles.has(candidate) && filename(candidate.name) === filename(source));
      if (file) { matches.set(source, file); unusedFiles.delete(file); }
    });
    const unmatched = missing.filter((source) => !matches.has(source));
    const embedded = rtfImages(rtf);
    const remaining = [...unusedFiles];
    const ordered = embedded.length === missing.length ? embedded : remaining;
    const orderedSources = embedded.length === missing.length ? missing : unmatched;
    if (ordered.length === orderedSources.length) orderedSources.forEach((source, index) => matches.set(source, ordered[index]));
    // Browsers may supply the bytes of a single web image alongside its HTML.
    if (sources.length === 1 && files.length === 1 && !matches.size && !/^data:/i.test(sources[0])) matches.set(sources[0], files[0]);

    let failed = false;
    const resolved = new Map();
    // Sequential reads bound memory and avoid starting many downloads on one paste.
    for (let i = 0; i < sources.length; i += 1) {
      const source = sources[i];
      if (disposed) return false;
      try {
        // eslint-disable-next-line no-await-in-loop
        resolved.set(source, await (matches.has(source) ? readDataURL(matches.get(source)) : resolveSource(source)));
      } catch (error) {
        failed = true;
      }
    }
    images.forEach((image) => {
      const source = image.getAttribute(SOURCE_ATTRIBUTE) || image.getAttribute('src') || '';
      if (resolved.has(source)) {
        image.setAttribute('src', resolved.get(source));
        image.removeAttribute(SOURCE_ATTRIBUTE);
        image.removeAttribute('srcset');
        image.removeAttribute('data-mce-src');
      } else {
        image.setAttribute(SOURCE_ATTRIBUTE, source);
      }
    });
    return !failed;
  };

  editor.on('PastePlainTextToggle', (event) => { pasteAsText = event.state; });
  editor.on('keydown', (event) => { shiftPaste = (event.ctrlKey || event.metaKey) && event.shiftKey && event.keyCode === 86; });
  editor.on('keyup', () => { shiftPaste = false; });
  editor.on('PastePostProcess', (event) => {
    const walker = document.createTreeWalker(event.node, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    nodes.forEach((node) => {
      const pattern = /LMCLIPIMAGE\d+X\d+END/g;
      const text = node.nodeValue;
      let cursor = 0;
      let match = pattern.exec(text);
      while (match) {
        const image = protectedImages.get(match[0]);
        if (image) {
          node.before(document.createTextNode(text.slice(cursor, match.index)), image.cloneNode(true));
          cursor = match.index + match[0].length;
        }
        match = pattern.exec(text);
      }
      if (cursor) node.replaceWith(document.createTextNode(text.slice(cursor)));
    });
  });

  editor.on('paste', (event) => {
    const clipboard = event.clipboardData;
    if (!clipboard || event.isDefaultPrevented() || editor.mode.isReadOnly() || pasteAsText || shiftPaste) return;
    const html = clipboard.getData('text/html');
    const text = clipboard.getData('text/plain');
    const rtf = clipboard.getData('text/rtf');
    const files = Array.from(clipboard.files || []).filter((file) => file.type.startsWith('image/'));
    if (!files.length) {
      Array.from(clipboard.items || []).forEach((item) => {
        if (item.kind === 'file' && item.type.startsWith('image/')) {
          const file = item.getAsFile();
          if (file) files.push(file);
        }
      });
    }
    const container = document.createElement('template');
    container.innerHTML = html.replace(/<!--\[if !vml\]>([\s\S]*?)<!\[endif\]-->/gi, '$1');
    if (!html && text) {
      const paragraph = document.createElement('p');
      paragraph.textContent = text;
      paragraph.style.whiteSpace = 'pre-wrap';
      container.content.appendChild(paragraph);
    }
    const images = Array.from(container.content.querySelectorAll('img'));
    images.forEach((image) => {
      if (!image.getAttribute('src') && image.getAttribute('data-src')) image.setAttribute('src', image.getAttribute('data-src'));
    });
    if (!images.length && files.length && (html || text)) {
      files.forEach((file, index) => {
        const image = document.createElement('img');
        image.src = `file:///clipboard/${index}/${encodeURIComponent(file.name)}`;
        image.alt = file.name;
        container.content.appendChild(image);
        images.push(image);
      });
    }
    if (!images.length) return;
    event.preventDefault();
    // Let TinyMCE's native handler remove its hidden paste bin and restore selection.
    pending = pending.then(async () => {
      if (disposed) return;
      const bookmark = editor.selection.getBookmark();
      editor.setProgressState(true);
      try {
        const complete = await importImages(images, files, rtf);
        if (disposed) return;
        sequence += 1;
        images.forEach((image, index) => {
          const token = `LMCLIPIMAGE${sequence}X${index}END`;
          protectedImages.set(token, image);
          image.replaceWith(document.createTextNode(token));
        });
        editor.selection.moveToBookmark(bookmark);
        // TinyMCE 5's paste command takes `content` (newer versions use `html`).
        editor.execCommand('mceInsertClipboardContent', false, { content: container.innerHTML });
        if (!complete) onError();
      } finally {
        protectedImages = new Map();
        if (!disposed) editor.setProgressState(false);
      }
    }).catch(() => { if (!disposed) onError(); });
  }, true);

  editor.on('remove', () => {
    disposed = true;
    requests.forEach((controller) => controller.abort());
  });

  return {
    async prepare() {
      await pending;
      if (disposed) return false;
      const images = Array.from(editor.getBody().querySelectorAll(`img[${SOURCE_ATTRIBUTE}]`));
      if (!images.length) return true;
      const complete = await importImages(images);
      if (!disposed) editor.nodeChanged();
      return complete && !disposed;
    },
  };
}
