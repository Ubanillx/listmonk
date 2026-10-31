import { t } from '../../i18n';
import React from 'react';

import { CodeOutlined, DataObjectOutlined, EditOutlined, PreviewOutlined } from '@mui/icons-material';
import { Tab, Tabs, Tooltip } from '@mui/material';

import { setSelectedMainTab, useSelectedMainTab } from '../../documents/editor/EditorContext';

export default function MainTabsGroup() {
  const selectedMainTab = useSelectedMainTab();
  const handleChange = (_: unknown, v: unknown) => {
    switch (v) {
      case 'json':
      case 'preview':
      case 'editor':
      case 'html':
        setSelectedMainTab(v);
        return;
      default:
        setSelectedMainTab('editor');
    }
  };

  return (
    <Tabs value={selectedMainTab} onChange={handleChange}>
      <Tab
        value="editor" aria-label={t('Edit')}
        label={
          <Tooltip title={t("Edit")}>
            <EditOutlined fontSize="small" />
          </Tooltip>
        }
      />
      <Tab
        value="preview" aria-label={t('Preview')}
        label={
          <Tooltip title={t("Preview")}>
            <PreviewOutlined fontSize="small" />
          </Tooltip>
        }
      />
      <Tab
        value="html" aria-label={t('HTML output')}
        label={
          <Tooltip title={t("HTML output")}>
            <CodeOutlined fontSize="small" />
          </Tooltip>
        }
      />
      <Tab
        value="json" aria-label={t('JSON output')}
        label={
          <Tooltip title={t("JSON output")}>
            <DataObjectOutlined fontSize="small" />
          </Tooltip>
        }
      />
    </Tabs>
  );
}
