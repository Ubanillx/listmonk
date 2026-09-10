import { Stack, useTheme } from '@mui/material';
import React, { useEffect, useRef } from 'react';

import { TEditorConfiguration } from '../documents/editor/core';
import { setDocument, subscribeDocument, useInspectorDrawerOpen, useSamplesDrawerOpen } from '../documents/editor/EditorContext';
import { renderHtmlWithMeta } from '../utils';
import InspectorDrawer, { INSPECTOR_DRAWER_WIDTH } from './InspectorDrawer';
import TemplatePanel from './TemplatePanel';

export const DEFAULT_SOURCE: TEditorConfiguration = {
  "root": {
    "type": "EmailLayout",
    "data": {}
  }
}

function useDrawerTransition(cssProperty: 'margin-left' | 'margin-right', open: boolean) {
  const { transitions } = useTheme();
  return transitions.create(cssProperty, {
    easing: !open ? transitions.easing.sharp : transitions.easing.easeOut,
    duration: !open ? transitions.duration.leavingScreen : transitions.duration.enteringScreen,
  });
}

export interface AppProps {
  // Initial configuration to load. Optional.
  data?: TEditorConfiguration,
  // Callback for any change in document. Optional.
  onChange?: (json: TEditorConfiguration, html: String) => void,
  // Optional height for the Stack component.
  height?: string,
}

export default function App(props: AppProps) {
  const inspectorDrawerOpen = useInspectorDrawerOpen();
  const samplesDrawerOpen = useSamplesDrawerOpen();

  const marginLeftTransition = useDrawerTransition('margin-left', samplesDrawerOpen);
  const marginRightTransition = useDrawerTransition('margin-right', inspectorDrawerOpen);

  const { data, onChange } = props;

  // Load the initial document once, when the incoming data actually changes
  // content. This used to run in the render body, so any re-render — toggling a
  // drawer is enough, and StrictMode doubles renders in development — wrote the
  // initial props back over whatever the user had edited. Comparing serialized
  // content rather than object identity also protects against a parent that
  // passes an equivalent-but-new object on every render.
  const seededRef = useRef<string | null>(null);
  useEffect(() => {
    const seeded = JSON.stringify(data ?? DEFAULT_SOURCE);
    if (seededRef.current === seeded) {
      return;
    }
    seededRef.current = seeded;
    setDocument(data ?? DEFAULT_SOURCE);
  }, [data]);

  // Keep the newest callback in a ref so a parent that recreates the function on
  // every render does not churn subscriptions, and register the subscription
  // exactly once, unhooking it on unmount. Subscriptions used to be appended on
  // every render and never released.
  const onChangeRef = useRef(onChange);
  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    const unsubscribe = subscribeDocument((document) => {
      onChangeRef.current?.(document, renderHtmlWithMeta(document, { rootBlockId: 'root' }));
    });

    return () => {
      unsubscribe?.();
    };
  }, []);

  return (
    <>
      <InspectorDrawer />

      <Stack
        sx={{
          marginRight: inspectorDrawerOpen ? `${INSPECTOR_DRAWER_WIDTH}px` : 0,
          transition: [marginLeftTransition, marginRightTransition].join(', '),
          height: props.height ? props.height : 'auto',
        }}
      >
        <TemplatePanel />
      </Stack>
    </>
  );
}
