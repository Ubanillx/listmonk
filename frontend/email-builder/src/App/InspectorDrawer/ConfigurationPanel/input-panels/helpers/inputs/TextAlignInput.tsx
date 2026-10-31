import { t } from '../../../../../../i18n';
import React, { useState } from 'react';

import { FormatAlignCenterOutlined, FormatAlignLeftOutlined, FormatAlignRightOutlined } from '@mui/icons-material';
import { ToggleButton } from '@mui/material';

import RadioGroupInput from './RadioGroupInput';

type Props = {
  label: string;
  defaultValue: string | null;
  onChange: (value: string | null) => void;
};
export default function TextAlignInput({ label, defaultValue, onChange }: Props) {
  const [value, setValue] = useState(defaultValue ?? 'left');

  return (
    <RadioGroupInput
      label={label}
      defaultValue={value}
      onChange={(value) => {
        setValue(value);
        onChange(value);
      }}
    >
      <ToggleButton value="left" aria-label={t('Align left')}>
        <FormatAlignLeftOutlined fontSize="small" />
      </ToggleButton>
      <ToggleButton value="center" aria-label={t('Align center')}>
        <FormatAlignCenterOutlined fontSize="small" />
      </ToggleButton>
      <ToggleButton value="right" aria-label={t('Align right')}>
        <FormatAlignRightOutlined fontSize="small" />
      </ToggleButton>
    </RadioGroupInput>
  );
}
