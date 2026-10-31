import React from 'react';
import { t } from '../../i18n';

import { AppRegistrationOutlined, LastPageOutlined } from '@mui/icons-material';
import { IconButton } from '@mui/material';

import { toggleInspectorDrawerOpen, useInspectorDrawerOpen } from '../../documents/editor/EditorContext';

export default function ToggleInspectorPanelButton() {
  const inspectorDrawerOpen = useInspectorDrawerOpen();

  const handleClick = () => {
    toggleInspectorDrawerOpen();
  };
  if (inspectorDrawerOpen) {
    return (
      <IconButton onClick={handleClick} aria-label={t('Hide settings')} title={t('Hide settings')}>
        <LastPageOutlined fontSize="small" />
      </IconButton>
    );
  }
  return (
    <IconButton onClick={handleClick} aria-label={t('Show settings')} title={t('Show settings')}>
      <AppRegistrationOutlined fontSize="small" />
    </IconButton>
  );
}
