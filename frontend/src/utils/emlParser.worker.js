import PostalMime from 'postal-mime';

/* eslint-disable no-restricted-globals */
self.onmessage = async ({ data }) => {
  try {
    const email = await PostalMime.parse(data, {
      maxNestingDepth: 20,
      maxHeadersSize: 256 * 1024,
      maxPartCount: 200,
      forceRfc822Attachments: true,
    });
    self.postMessage({ html: email.html, text: email.text, attachments: email.attachments });
  } catch (error) {
    self.postMessage({ error: 'invalid' });
  }
};
