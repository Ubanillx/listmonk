import { t } from '../../../../i18n';
import React, { useState } from 'react';

import { ToggleButton } from '@mui/material';
import { ButtonProps, ButtonPropsDefaults, ButtonPropsSchema } from '@usewaypoint/block-button';

import BaseSidebarPanel from './helpers/BaseSidebarPanel';
import ColorInput from './helpers/inputs/ColorInput';
import RadioGroupInput from './helpers/inputs/RadioGroupInput';
import TextInput from './helpers/inputs/TextInput';
import MultiStylePropertyPanel from './helpers/style-inputs/MultiStylePropertyPanel';

type ButtonSidebarPanelProps = {
  data: ButtonProps;
  setData: (v: ButtonProps) => void;
};
export default function ButtonSidebarPanel({ data, setData }: ButtonSidebarPanelProps) {
  const [, setErrors] = useState<Zod.ZodError | null>(null);

  const updateData = (d: unknown) => {
    const res = ButtonPropsSchema.safeParse(d);
    if (res.success) {
      setData(res.data);
      setErrors(null);
    } else {
      setErrors(res.error);
    }
  };

  const text = data.props?.text ?? ButtonPropsDefaults.text;
  const url = data.props?.url ?? ButtonPropsDefaults.url;
  const fullWidth = data.props?.fullWidth ?? ButtonPropsDefaults.fullWidth;
  const size = data.props?.size ?? ButtonPropsDefaults.size;
  const buttonStyle = data.props?.buttonStyle ?? ButtonPropsDefaults.buttonStyle;
  const buttonTextColor = data.props?.buttonTextColor ?? ButtonPropsDefaults.buttonTextColor;
  const buttonBackgroundColor = data.props?.buttonBackgroundColor ?? ButtonPropsDefaults.buttonBackgroundColor;

  return (
    <BaseSidebarPanel title={t("Button block")}>
      <TextInput
        label={t("Button text")}
        defaultValue={text}
        onChange={(text) => updateData({ ...data, props: { ...data.props, text } })}
      />
      <TextInput
        label={t("Url")}
        defaultValue={url}
        onChange={(url) => updateData({ ...data, props: { ...data.props, url } })}
      />
      <RadioGroupInput
        label={t("Width")}
        defaultValue={fullWidth ? 'FULL_WIDTH' : 'AUTO'}
        onChange={(v) => updateData({ ...data, props: { ...data.props, fullWidth: v === 'FULL_WIDTH' } })}
      >
        <ToggleButton value="FULL_WIDTH">{t("Full")}</ToggleButton>
        <ToggleButton value="AUTO">{t("Auto")}</ToggleButton>
      </RadioGroupInput>
      <RadioGroupInput
        label={t("Size")}
        defaultValue={size}
        onChange={(size) => updateData({ ...data, props: { ...data.props, size } })}
      >
        <ToggleButton value="x-small">{t("Xs")}</ToggleButton>
        <ToggleButton value="small">{t("Sm")}</ToggleButton>
        <ToggleButton value="medium">{t("Md")}</ToggleButton>
        <ToggleButton value="large">{t("Lg")}</ToggleButton>
      </RadioGroupInput>
      <RadioGroupInput
        label={t("Style")}
        defaultValue={buttonStyle}
        onChange={(buttonStyle) => updateData({ ...data, props: { ...data.props, buttonStyle } })}
      >
        <ToggleButton value="rectangle">{t("Rectangle")}</ToggleButton>
        <ToggleButton value="rounded">{t("Rounded")}</ToggleButton>
        <ToggleButton value="pill">{t("Pill")}</ToggleButton>
      </RadioGroupInput>
      <ColorInput
        label={t("Text color")}
        defaultValue={buttonTextColor}
        onChange={(buttonTextColor) => updateData({ ...data, props: { ...data.props, buttonTextColor } })}
      />
      <ColorInput
        label={t("Button color")}
        defaultValue={buttonBackgroundColor}
        onChange={(buttonBackgroundColor) => updateData({ ...data, props: { ...data.props, buttonBackgroundColor } })}
      />
      <MultiStylePropertyPanel
        names={['backgroundColor', 'fontFamily', 'fontSize', 'fontWeight', 'textAlign', 'padding']}
        value={data.style}
        onChange={(style) => updateData({ ...data, style })}
      />
    </BaseSidebarPanel>
  );
}
