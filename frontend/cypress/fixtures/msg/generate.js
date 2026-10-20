// Synthetic Outlook MSG fixtures with no real mailbox data. Run with Node from this directory.
const fs = require('fs');
const path = require('path');
const { CFB } = require('xlsx');
const iconv = require('iconv-lite');

function message(name, {
  html, text, unicodeHtml, codepage = 65001, missing = false,
} = {}) {
  const file = CFB.utils.cfb_new();
  const add = (key, bytes) => CFB.utils.cfb_add(file, key, bytes);
  const unicode = (key, value) => add(key, Buffer.from(`${value}\0`, 'utf16le'));
  const props = Buffer.alloc(48);
  props.writeUInt32LE(0x3fde0003, 32);
  props.writeUInt32LE(codepage, 40);
  add('__properties_version1.0', props);
  unicode('__substg1.0_001A001F', 'IPM.Note');
  unicode('__substg1.0_0037001F', 'Do not import this subject');
  if (text) unicode('__substg1.0_1000001F', text);
  if (unicodeHtml) unicode('__substg1.0_1013001F', unicodeHtml);
  if (html) {
    add('__substg1.0_10130102', iconv.encode(html, codepage === 65001 ? 'utf-8' : 'gb18030'));
    if (!missing) {
      const attachment = '__attach_version1.0_#00000000/';
      add(`${attachment}__properties_version1.0`, Buffer.alloc(8));
      unicode(`${attachment}__substg1.0_3707001F`, 'inline.png');
      unicode(`${attachment}__substg1.0_3712001F`, 'inline@example');
      unicode(`${attachment}__substg1.0_370E001F`, 'image/png');
      add(`${attachment}__substg1.0_37010102`, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64'));
    }
    const attachment = '__attach_version1.0_#00000001/';
    add(`${attachment}__properties_version1.0`, Buffer.alloc(8));
    unicode(`${attachment}__substg1.0_3707001F`, 'ignored.pdf');
    unicode(`${attachment}__substg1.0_370E001F`, 'application/pdf');
    add(`${attachment}__substg1.0_37010102`, Buffer.from('Synthetic ordinary attachment'));
  }
  fs.writeFileSync(path.join(__dirname, name), CFB.write(file, { type: 'buffer' }));
}

message('html.msg', {
  html: '<p>你好 MSG Before</p><table style="width:300px"><tr><td><img src="cid:inline%40example" alt="first"></td></tr></table>'
    + '<p>After</p><img src="cid:inline@example" alt="repeated"><script>window.msgExecuted = true;</script>',
  text: 'Do not import fallback',
});
message('plain.msg', { text: '你好 MSG\r\nPlain <text> & content' });
message('legacy.msg', { html: '<p>你好 GB18030</p>', codepage: 54936 });
message('unicode-html.msg', { unicodeHtml: '<p>你好 Unicode HTML</p>' });
message('missing.msg', { html: '<p>Broken MSG</p><img src="cid:missing@example">', missing: true });
message('empty.msg');
