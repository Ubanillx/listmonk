import { t } from '../../i18n';
import React from 'react';

import {
  Box, Button, Drawer, Tab, Tabs,
} from '@mui/material';
import { ArrowBackOutlined } from '@mui/icons-material';

import { setSidebarTab, toggleInspectorDrawerOpen, useInspectorDrawerOpen, useSelectedSidebarTab } from '../../documents/editor/EditorContext';

import ConfigurationPanel from './ConfigurationPanel';
import StylesPanel from './StylesPanel';

export const INSPECTOR_DRAWER_WIDTH = 320;

export default function InspectorDrawer() {
  const selectedSidebarTab = useSelectedSidebarTab();
  const inspectorDrawerOpen = useInspectorDrawerOpen();

  const renderCurrentSidebarPanel = () => {
    switch (selectedSidebarTab) {
      case 'block-configuration':
        return <ConfigurationPanel />;
      case 'styles':
        return <StylesPanel />;
    }
  };

  return (
    <Drawer
      variant="persistent"
      anchor="right"
      className="sidebar"
      open={inspectorDrawerOpen}
      sx={{
        width: { xs: 0, md: inspectorDrawerOpen ? INSPECTOR_DRAWER_WIDTH : 0 },
      }}
      // Make the drawer relative to the wrapper instead of body.
      PaperProps={{ sx: { position: 'absolute', zIndex: 2, maxWidth: '100%', width: { xs: '100%', md: INSPECTOR_DRAWER_WIDTH },
        top: { xs: 49, md: 0 }, height: { xs: 'calc(100% - 49px)', md: '100%' } } }}
      ModalProps={{
        container: document.querySelector('.email-builder-container'),
        style: { position: 'absolute', zIndex: 0 },
      }}
    >
      <Box sx={{
        width: '100%', height: { xs: 85, md: 49 }, flexShrink: 0, borderBottom: 1, borderColor: 'divider',
      }}
      >
        <Button startIcon={<ArrowBackOutlined fontSize="small" />} onClick={toggleInspectorDrawerOpen}
          sx={{ display: { xs: 'inline-flex', md: 'none' }, height: 36, ml: 1 }}>
          {t('Back to canvas')}
        </Button>
        <Box px={2}>
          <Tabs value={selectedSidebarTab} onChange={(_, v) => setSidebarTab(v)}>
            <Tab value="styles" label={t("Styles")} />
            <Tab value="block-configuration" label={t("Inspect")} />
          </Tabs>
        </Box>
      </Box>
      <Box sx={{ width: '100%', height: { xs: 'calc(100% - 85px)', md: 'calc(100% - 49px)' }, overflow: 'auto' }}>
        {renderCurrentSidebarPanel()}
      </Box>
    </Drawer>
  );
}
