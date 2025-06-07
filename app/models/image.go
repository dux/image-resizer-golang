package models

import (
  "image"
  "os"
  "path/filepath"
)

type Image struct {
  Source   string
  Width    int
  Height   int
  Format   string
  FileSize int64
}

func NewImage(src string) (*Image, error) {
  img := &Image{Source: src}

  file, err := os.Open(src)
  if err != nil {
    return nil, err
  }
  defer file.Close()

  config, format, err := image.DecodeConfig(file)
  if err != nil {
    return nil, err
  }

  stat, err := file.Stat()
  if err != nil {
    return nil, err
  }

  img.Width = config.Width
  img.Height = config.Height
  img.Format = format
  img.FileSize = stat.Size()

  return img, nil
}

func (img *Image) GetProperties() map[string]interface{} {
  return map[string]interface{}{
    "source":   img.Source,
    "width":    img.Width,
    "height":   img.Height,
    "format":   img.Format,
    "fileSize": img.FileSize,
    "filename": filepath.Base(img.Source),
  }
}

func (img *Image) AspectRatio() float64 {
  if img.Height == 0 {
    return 0
  }
  return float64(img.Width) / float64(img.Height)
}

func (img *Image) IsLandscape() bool {
  return img.Width > img.Height
}

func (img *Image) IsPortrait() bool {
  return img.Height > img.Width
}

func (img *Image) IsSquare() bool {
  return img.Width == img.Height
}
