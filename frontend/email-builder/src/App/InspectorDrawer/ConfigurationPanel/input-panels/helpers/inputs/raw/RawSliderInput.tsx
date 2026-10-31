import React from 'react';

import { Box, Slider, Stack, Typography } from '@mui/material';

type SliderInputProps = {
  iconLabel: JSX.Element;
  label?: string;

  step?: number;
  marks?: boolean;
  units: string;
  min?: number;
  max?: number;

  value: number;
  setValue: (v: number) => void;
};

export default function RawSliderInput({ iconLabel, value, setValue, units, label, ...props }: SliderInputProps) {
  return (
    <Stack direction="row" alignItems="center" spacing={2} justifyContent="space-between" width="100%">
      <Box sx={{ minWidth: 24, lineHeight: 1, flexShrink: 0 }}>{iconLabel}</Box>
      <Slider aria-label={label}
        {...props}
        value={value}
        onChange={(_, value: unknown) => {
          if (typeof value !== 'number') {
            throw new Error('RawSliderInput values can only receive numeric values');
          }
          setValue(value);
        }}
      />
      <Box sx={{ minWidth: 32, textAlign: 'right', flexShrink: 0 }}>
        <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1 }}>
          {value}
          {units}
        </Typography>
      </Box>
    </Stack>
  );
}
