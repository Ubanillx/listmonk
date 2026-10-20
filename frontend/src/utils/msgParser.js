import MsgReader from '@kenjiuno/msgreader';
import iconv from 'iconv-lite';

const MAX_BYTES = 32 * 1024 * 1024;
const encodings = {
  65001: 'utf-8', 65000: 'utf7', 1200: 'utf16le', 1201: 'utf16be', 54936: 'gb18030', 20127: 'ascii',
};
const mimeTypes = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', bmp: 'image/bmp', avif: 'image/avif',
};

// Normalize Outlook properties to the same shape as Postal MIME. Nested emails
// remain ordinary attachments and never become part of the imported body.
export default function parseMsg(bytes) {
  let reader = new MsgReader(bytes);
  let email = reader.getFileData();
  if (email.error || email.dataType !== 'msg') throw new Error('invalid');
  const codepage = email.internetCodepage || email.messageCodepage;
  const encoding = encodings[codepage] || (codepage ? `cp${codepage}` : 'utf-8');
  if (codepage && iconv.encodingExists(encoding)) {
    reader = new MsgReader(bytes);
    reader.parserConfig = { ansiEncoding: encoding };
    email = reader.getFileData();
  }
  let html = email.bodyHtml;
  if (!html && email.html?.length) {
    // Outlook usually stores HTML as binary with an Internet code page. A
    // charset declaration also covers exporters that omit that property.
    const meta = new TextDecoder('windows-1252').decode(email.html.slice(0, 4096)).match(/charset\s*=\s*["']?([\w-]+)/i);
    const charset = codepage ? encoding : (meta?.[1] || 'utf-8');
    if (!iconv.encodingExists(charset)) throw new Error('invalid');
    html = iconv.decode(email.html, charset).replace(/\0+$/, '');
  }
  const parts = email.attachments || [];
  if (parts.length > 200) throw new Error('size');
  let total = 0;
  const attachments = parts.map((part) => {
    const filename = part.fileName || part.fileNameShort || part.name || '';
    if (part.innerMsgContent) return { filename };
    const { content } = reader.getAttachment(part);
    total += content.byteLength;
    if (total > MAX_BYTES) throw new Error('size');
    const extension = filename.split('.').pop().toLowerCase();
    return {
      filename,
      contentId: part.pidContentId,
      mimeType: (part.attachMimeTag || mimeTypes[extension] || 'application/octet-stream').split(';')[0].trim(),
      content,
    };
  });
  return { html, text: email.body, attachments };
}
