import { t } from '../../../i18n';
import React, { useState } from 'react';

import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
  Typography,
} from '@mui/material';

import { resetDocument } from '../../../documents/editor/EditorContext';

import validateJsonStringValue from './validateJsonStringValue';

type ImportJsonDialogProps = {
  onClose: () => void;
};
export default function ImportJsonDialog({ onClose }: ImportJsonDialogProps) {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleChange: React.ChangeEventHandler<HTMLTextAreaElement | HTMLInputElement> = (ev) => {
    const v = ev.currentTarget.value;
    setValue(v);
    const { error } = validateJsonStringValue(v);
    setError(error ?? null);
  };

  let errorAlert = null;
  if (error) {
    errorAlert = <Alert color="error">{t(error)}</Alert>;
  }

  return (
    <Dialog open onClose={onClose}>
      <DialogTitle>{t("Import JSON")}</DialogTitle>
      <form
        onSubmit={(ev) => {
          ev.preventDefault();
          const { error, data } = validateJsonStringValue(value);
          setError(error ?? null);
          if (!data) {
            return;
          }
          resetDocument(data);
          onClose();
        }}
      >
        <DialogContent>
          <Typography color="text.secondary" paragraph>
            {t('Paste the template JSON below.')}
          </Typography>
          {errorAlert}
          <TextField
            label={t('JSON output')}
            error={error !== null}
            value={value}
            onChange={handleChange}
            type="text"
            helperText={t("This will override your current template.")}
            variant="outlined"
            fullWidth
            rows={10}
            multiline
          />
        </DialogContent>
        <DialogActions>
          <Button type="button" onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button variant="contained" type="submit" disabled={error !== null}>
            {t('Import')}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
