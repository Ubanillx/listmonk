import parseMsg from './msgParser';

/* eslint-disable no-restricted-globals */
self.onmessage = ({ data }) => {
  try {
    self.postMessage(parseMsg(data));
  } catch (error) {
    self.postMessage({ error: error.message === 'size' ? 'size' : 'invalid' });
  }
};
