package db

import (
	"fmt"
	"strings"

	"github.com/k0kubun/pp"
	"github.com/kotakanbe/goval-dictionary/config"
	"github.com/kotakanbe/goval-dictionary/log"
	"github.com/kotakanbe/goval-dictionary/models"
)

// RedHat is a struct for DBAccess
type RedHat struct {
	Base
}

// NewRedHat creates DBAccess
func NewRedHat() RedHat {
	d := RedHat{
		Base{
			Family: config.RedHat,
		},
	}
	d.OpenDB()
	return d
}

// InsertOval inserts RedHat OVAL
func (o RedHat) InsertOval(root *models.Root, meta models.FetchMeta) error {
	tx := o.DB.Begin()

	oldmeta := models.FetchMeta{}
	r := tx.Where(&models.FetchMeta{FileName: meta.FileName}).First(&oldmeta)
	if !r.RecordNotFound() && oldmeta.Timestamp.Equal(meta.Timestamp) {
		log.Infof("  Skip %s %s (Same Timestamp)", root.Family, root.OSVersion)
		return nil
	}
	log.Infof("  Refreshing %s %s...", root.Family, root.OSVersion)

	old := models.Root{}
	r = tx.Where(&models.Root{Family: root.Family, OSVersion: root.OSVersion}).First(&old)
	if !r.RecordNotFound() {
		// Delete data related to root passed in arg
		defs := []models.Definition{}
		o.DB.Model(&old).Related(&defs, "Definitions")
		for _, def := range defs {
			adv := models.Advisory{}
			o.DB.Model(&def).Related(&adv, "Avisory")
			if err := tx.Unscoped().Where("advisory_id = ?", adv.ID).Delete(&models.Cve{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
			if err := tx.Unscoped().Where("advisory_id = ?", adv.ID).Delete(&models.Bugzilla{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
			if err := tx.Unscoped().Where("advisory_id = ?", adv.ID).Delete(&models.Cpe{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
			if err := tx.Unscoped().Where("definition_id = ?", def.ID).Delete(&models.Advisory{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
			if err := tx.Unscoped().Where("definition_id= ?", def.ID).Delete(&models.Package{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
			if err := tx.Unscoped().Where("definition_id = ?", def.ID).Delete(&models.Reference{}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("Failed to delete: %s", err)
			}
		}
		if err := tx.Unscoped().Where("root_id = ?", old.ID).Delete(&models.Definition{}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("Failed to delete: %s", err)
		}
		if err := tx.Unscoped().Where("id = ?", old.ID).Delete(&models.Root{}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("Failed to delete: %s", err)
		}
	}

	if err := tx.Create(&root).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("Failed to insert. cve: %s, err: %s",
			pp.Sprintf("%v", root), err)
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

// GetByPackName select definitions by packName
func (o RedHat) GetByPackName(osVer, packName string) ([]models.Definition, error) {
	osVer = major(osVer)
	packs := []models.Package{}
	if err := o.DB.Where(&models.Package{Name: packName}).Find(&packs).Error; err != nil {
		return nil, err
	}

	defs := []models.Definition{}
	for _, p := range packs {
		def := models.Definition{}
		if err := o.DB.Where("id = ?", p.DefinitionID).Find(&def).Error; err != nil {
			return nil, err
		}

		root := models.Root{}
		if err := o.DB.Where("id = ?", def.RootID).Find(&root).Error; err != nil {
			return nil, err
		}

		if root.Family == config.RedHat && major(root.OSVersion) == osVer {
			defs = append(defs, def)
		}
	}

	for i, def := range defs {
		adv := models.Advisory{}
		if err := o.DB.Model(&def).Related(&adv, "Advisory").Error; err != nil {
			return nil, err
		}

		cves := []models.Cve{}
		if err := o.DB.Model(&adv).Related(&cves, "Cves").Error; err != nil {
			return nil, err
		}
		adv.Cves = cves

		bugs := []models.Bugzilla{}
		if err := o.DB.Model(&adv).Related(&bugs, "Bugzillas").Error; err != nil {
			return nil, err
		}
		adv.Bugzillas = bugs

		cpes := []models.Cpe{}
		if err := o.DB.Model(&adv).Related(&cpes, "AffectedCPEList").Error; err != nil {
			return nil, err
		}
		adv.AffectedCPEList = cpes

		defs[i].Advisory = adv

		packs := []models.Package{}
		if err := o.DB.Model(&def).Related(&packs, "AffectedPacks").Error; err != nil {
			return nil, err
		}
		defs[i].AffectedPacks = filterByMajor(packs, osVer)

		refs := []models.Reference{}
		if err := o.DB.Model(&def).Related(&refs, "References").Error; err != nil {
			return nil, err
		}
		defs[i].References = refs
	}

	return defs, nil
}

// GetByCveID select definitions by CveID
func (o RedHat) GetByCveID(osVer, cveID string) ([]models.Definition, error) {
	osVer = major(osVer)
	cves := []models.Cve{}
	if err := o.DB.Where(&models.Cve{CveID: cveID}).Find(&cves).Error; err != nil {
		return nil, err
	}

	defs := []models.Definition{}
	for _, cve := range cves {
		adv := models.Advisory{}
		if err := o.DB.Where("id = ?", cve.AdvisoryID).Find(&adv).Error; err != nil {
			return nil, err
		}

		def := models.Definition{}
		if err := o.DB.Where("id = ?", adv.DefinitionID).Find(&def).Error; err != nil {
			return nil, err
		}

		root := models.Root{}
		if err := o.DB.Where("id = ?", def.RootID).Find(&root).Error; err != nil {
			return nil, err
		}
		if root.Family == config.RedHat && major(root.OSVersion) == osVer {
			defs = append(defs, def)
		}
	}

	for i, def := range defs {
		adv := models.Advisory{}
		if err := o.DB.Model(&def).Related(&adv, "Advisory").Error; err != nil {
			return nil, err
		}

		cves := []models.Cve{}
		if err := o.DB.Model(&adv).Related(&cves, "Cves").Error; err != nil {
			return nil, err
		}
		adv.Cves = cves

		bugs := []models.Bugzilla{}
		if err := o.DB.Model(&adv).Related(&bugs, "Bugzillas").Error; err != nil {
			return nil, err
		}
		adv.Bugzillas = bugs

		cpes := []models.Cpe{}
		if err := o.DB.Model(&adv).Related(&cpes, "AffectedCPEList").Error; err != nil {
			return nil, err
		}
		adv.AffectedCPEList = cpes

		defs[i].Advisory = adv

		packs := []models.Package{}
		if err := o.DB.Model(&def).Related(&packs, "AffectedPacks").Error; err != nil {
			return nil, err
		}
		defs[i].AffectedPacks = filterByMajor(packs, osVer)

		refs := []models.Reference{}
		if err := o.DB.Model(&def).Related(&refs, "References").Error; err != nil {
			return nil, err
		}
		defs[i].References = refs
	}

	return defs, nil
}

func filterByMajor(packs []models.Package, majorVer string) (filtered []models.Package) {
	for _, p := range packs {
		if strings.Contains(p.Version, ".el"+majorVer) {
			filtered = append(filtered, p)
		}
	}
	return
}
